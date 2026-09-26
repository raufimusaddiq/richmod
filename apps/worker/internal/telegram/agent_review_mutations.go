package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// agentResolveReview is retained for internal callers/tests, but review
// resolution is always server-bound. No conversational path may select the
// newest household review as a mutation target.
func (p *Processor) agentResolveReview(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	return p.agentResolveBoundReview(ctx, state, call, args)
}

type agentTransactionReview struct {
	reviewID, transactionID, transactionType, reviewType, conversationState, merchantID string
	messageID                                                                           int64
	ambiguous                                                                           bool
	count                                                                               int
}

func (p *Processor) agentBoundTransactionReview(ctx context.Context, state *agentState) (*agentTransactionReview, error) {
	query := `SELECT r.id,r.transaction_id,t.type,r.review_type,c.state,COALESCE(t.merchant_id::text,''),COALESCE(rr.telegram_message_id,0)
		FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id
		WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND rr.telegram_chat_id=$2 ORDER BY r.created_at DESC LIMIT 2`
	params := []any{state.HouseholdID, state.Update.Message.Chat.ID}
	if state.Update.Message.ReplyToMessage != nil {
		query = `SELECT r.id,r.transaction_id,t.type,r.review_type,c.state,COALESCE(t.merchant_id::text,''),COALESCE(rr.telegram_message_id,0)
			FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 LIMIT 2`
		params = append(params, state.Update.Message.ReplyToMessage.MessageID)
	}
	rows, err := p.pool.Query(ctx, query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var choices []agentTransactionReview
	for rows.Next() {
		var v agentTransactionReview
		if err := rows.Scan(&v.reviewID, &v.transactionID, &v.transactionType, &v.reviewType, &v.conversationState, &v.merchantID, &v.messageID); err != nil {
			return nil, err
		}
		choices = append(choices, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(choices) == 0 {
		return nil, nil
	}
	if len(choices) != 1 {
		return &agentTransactionReview{ambiguous: true, count: len(choices)}, nil
	}
	return &choices[0], nil
}

func (p *Processor) agentCategoryID(ctx context.Context, householdID, slug string) (string, error) {
	if strings.TrimSpace(slug) == "" {
		return "", nil
	}
	var id string
	if err := p.pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND slug=$2 AND active`, householdID, slug).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (p *Processor) agentRejectTransactionReview(ctx context.Context, state *agentState, call gateway.ToolCall, review agentTransactionReview) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE transaction SET status='VOIDED',confirmed_at=NULL,voided_at=now(),updated_at=now() WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW'`, review.transactionID, state.HouseholdID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')`, review.transactionID); err != nil {
		return result, true, err
	}
	if err = resolveCanonicalReviewItem(ctx, tx, review.reviewID, userID, "TELEGRAM_REJECTED"); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.reviewID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event s SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='CONFIRMED') THEN 'PROCESSED' ELSE 'IGNORED' END WHERE s.id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, review.transactionID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification','REJECT')) ON CONFLICT DO NOTHING`, review.transactionID, state.SourceEventID, review.reviewID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'REJECT_REVIEW','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'agent_sprint',1))`, state.HouseholdID, userID, review.transactionID, review.reviewID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "RESOLVED"
	result.Mutation = map[string]any{"action": "REVIEW_IGNORED", "review_type": review.reviewType}
	return result, true, nil
}

func (p *Processor) agentSaveReviewField(ctx context.Context, state *agentState, call gateway.ToolCall, review agentTransactionReview, field, value string) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	rememberedCategoryID := ""
	if field == "merchant" {
		var merchantID string
		err = tx.QueryRow(ctx, `SELECT min(ma.normalized_merchant_id::text),min(ma.default_category_id::text) FROM merchant_alias ma JOIN category c ON c.id=ma.default_category_id WHERE ma.household_id=$1 AND lower(regexp_replace(btrim(ma.raw_name), '[[:space:]]+', ' ', 'g'))=lower(regexp_replace(btrim($2), '[[:space:]]+', ' ', 'g')) AND ma.auto_apply AND ma.created_from_user_confirmation AND c.household_id=$1 AND c.active GROUP BY ma.household_id,lower(regexp_replace(btrim(ma.raw_name), '[[:space:]]+', ' ', 'g')) HAVING count(DISTINCT ma.default_category_id)=1 AND count(DISTINCT ma.normalized_merchant_id)=1`, state.HouseholdID, value).Scan(&merchantID, &rememberedCategoryID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,regexp_replace(trim($2), '[[:space:]]+', ' ', 'g')) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=now() RETURNING id`, state.HouseholdID, value).Scan(&merchantID)
		}
		if err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction SET merchant_id=$2,updated_at=now() WHERE id=$1 AND household_id=$3 AND status='NEEDS_REVIEW'`, review.transactionID, merchantID, state.HouseholdID); err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction_proposal SET merchant_raw=$2,updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')`, review.transactionID, value); err != nil {
			return result, true, err
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE transaction SET description=$2,updated_at=now() WHERE id=$1 AND household_id=$3 AND status='NEEDS_REVIEW'`, review.transactionID, value, state.HouseholdID); err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction_proposal SET description=$2,updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')`, review.transactionID, value); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'field',$4::text,'value',$5::text)) ON CONFLICT DO NOTHING`, review.transactionID, state.SourceEventID, review.reviewID, field, value); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'UPDATE_REVIEW_DETAIL','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'field',$5::text,'value',$6::text,'agent_sprint',1))`, state.HouseholdID, userID, review.transactionID, review.reviewID, field, value); err != nil {
		return result, true, err
	}
	if rememberedCategoryID != "" {
		if err = p.agentConfirmReviewTx(ctx, tx, state, review, rememberedCategoryID, reviewExtraction{Confidence: 1}, userID); err != nil {
			var residual errReviewResidualFactsRequired
			if !errors.As(err, &residual) {
				return result, true, err
			}
			if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_CATEGORY',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.reviewID); err != nil {
				return result, true, err
			}
			if err = tx.Commit(ctx); err != nil {
				return result, true, err
			}
			result.Status = "NEEDS_REVIEW"
			result.Mutation = map[string]any{"action": "REVIEW_DETAIL_SAVED", "field": field, "value": value}
			result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": residual.facts}
			return result, true, nil
		}
		if err = tx.Commit(ctx); err != nil {
			return result, true, err
		}
		result.Status = "RESOLVED"
		result.Mutation = map[string]any{"action": "REVIEW_DETAIL_SAVED_AND_CONFIRMED", "field": field, "value": value}
		return result, true, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_CATEGORY',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.reviewID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "NEEDS_REVIEW"
	result.Mutation = map[string]any{"action": "REVIEW_DETAIL_SAVED", "field": field, "value": value}
	result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": []string{"category_slug"}}
	return result, true, nil
}

func (p *Processor) agentConfirmTransactionReview(ctx context.Context, state *agentState, call gateway.ToolCall, review agentTransactionReview, categoryID string, value reviewExtraction) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	if err = p.agentConfirmReviewTx(ctx, tx, state, review, categoryID, value, userID); err != nil {
		var residual errReviewResidualFactsRequired
		if errors.As(err, &residual) {
			result.Status = "RESIDUAL_FACTS_REQUIRED"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": residual.facts}
			return result, true, nil
		}
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	var conversationState string
	_ = p.pool.QueryRow(ctx, `SELECT state FROM review_conversation WHERE review_request_id=$1`, review.reviewID).Scan(&conversationState)
	result.Status = "RESOLVED"
	result.Mutation = map[string]any{"action": "REVIEW_CONFIRMED", "review_type": review.reviewType}
	if conversationState == "AWAITING_MERCHANT_DECISION" {
		result.Status = "AWAITING_MERCHANT_DECISION"
		result.Review = map[string]any{"required": true, "review_type": review.reviewType, "merchant_learning": true}
	}
	return result, true, nil
}

func (p *Processor) agentConfirmReviewTx(ctx context.Context, tx pgx.Tx, state *agentState, review agentTransactionReview, categoryID string, value reviewExtraction, userID string) error {
	var merchantID *string
	// IR-02: the conversational lane can reach the same canonical confirm as the
	// generic reply lane, so the stored residual contract is enforced here too.
	// The agent's native action handlers already refused undated or uncategorized
	// input, so only an explicitly supplied date satisfies the residual date.
	var storedDecision []byte
	if err := tx.QueryRow(ctx, `SELECT COALESCE(ri.decision,'{}'::jsonb) FROM review_item ri WHERE ri.id=(SELECT review_item_id FROM review_request WHERE id=$1 AND household_id=$2) FOR UPDATE OF ri`, review.reviewID, state.HouseholdID).Scan(&storedDecision); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	payDate, err := parseSuppliedReviewDate(value.PayDate)
	if err != nil {
		return err
	}
	if blocked := residualConfirmationBlockers(storedDecision, payDate != nil, categoryID != "", value.Note != ""); len(blocked) > 0 {
		return errReviewResidualFactsRequired{facts: blocked}
	}
	var transactionAt *time.Time
	if payDate != nil {
		at, err := time.ParseInLocation("2006-01-02", *payDate, jakartaLocation())
		if err != nil {
			return err
		}
		transactionAt = &at
	}
	// ADR-046: the conversational lane calls the same canonical confirm as the
	// generic reply lane. Confirm owns the transaction/proposal mutation and
	// candidate revalidation; Telegram keeps only conversation state, evidence,
	// and its own audit shape.
	confirmResult, err := reviewdomain.ConfirmTransactionReview(ctx, tx, reviewdomain.ConfirmCommand{
		HouseholdID: state.HouseholdID, ActorUserID: userID, TransactionID: review.transactionID,
		ReviewItemID: pendingReviewItemID(ctx, tx, review.reviewID),
		RequestID:    review.reviewID, Action: "TELEGRAM_CONFIRMED",
		CategorySupplied: categoryID != "", CategoryID: categoryID,
		Description: value.Description, Note: value.Note, TransactionAt: transactionAt,
		ResolveReview: false,
	})
	if err != nil {
		return err
	}
	if confirmResult.MerchantID != "" {
		merchantID = &confirmResult.MerchantID
	}
	// A confirmed review also completes the document workflow and any payslip
	// salary recording, both through the shared operations.
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, tx, review.transactionID); err != nil {
		return err
	}
	if value.PayDate != "" {
		facts, ok, err := reviewdomain.LoadPayslipFacts(ctx, tx, review.transactionID)
		if err != nil {
			return err
		}
		if ok && reviewPayrollPeriodPattern.MatchString(facts.Period) {
			if _, err := reviewdomain.RecordSalaryEvent(ctx, tx, reviewdomain.SalaryCommand{
				HouseholdID: state.HouseholdID, Employer: facts.Employer, Period: facts.Period,
				PayDate: value.PayDate, NetPay: facts.NetPay,
				Transaction: review.transactionID, SourceEvent: state.SourceEventID,
			}); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event s SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END WHERE s.id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, review.transactionID); err != nil {
		return err
	}
	askRemember := merchantID != nil && categoryID != ""
	if askRemember {
		// The review item completes now, not on the optional merchant answer: a
		// follow-up question the user may never send must not strand the review
		// (UIR-08). AWAITING_MERCHANT_DECISION marks the pending question without
		// depending on review_request.status staying OPEN.
		if err := resolveCanonicalReviewItem(ctx, tx, review.reviewID, userID, "TELEGRAM_CONFIRMED"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_MERCHANT_DECISION',context_json=context_json||jsonb_build_object('category_id',NULLIF($2,'')::uuid),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.reviewID, categoryID); err != nil {
			return err
		}
	} else {
		if err := resolveCanonicalReviewItem(ctx, tx, review.reviewID, userID, "TELEGRAM_CONFIRMED"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',context_json=context_json||jsonb_build_object('category_id',NULLIF($2,'')::uuid),last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.reviewID, categoryID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($2,$1,'TELEGRAM_REVIEW_REPLY',$3,jsonb_build_object('review_request_id',$4::uuid,'classification','CONFIRM')) ON CONFLICT DO NOTHING`, state.SourceEventID, review.transactionID, value.Confidence, review.reviewID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'RESOLVE_REVIEW','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'category_id',NULLIF($5,'')::uuid,'agent_sprint',1))`, state.HouseholdID, userID, review.transactionID, review.reviewID, categoryID); err != nil {
		return err
	}
	return nil
}

type errReviewResidualFactsRequired struct{ facts []string }

func (e errReviewResidualFactsRequired) Error() string { return strings.Join(e.facts, ",") }

func (p *Processor) agentResolveTransferClassification(ctx context.Context, state *agentState, call gateway.ToolCall, review agentTransactionReview, classification, wealthHint, categoryID string) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	wealthID := wealthHint
	if classification == "ASSET_PURCHASE" {
		id, e := resolveUniqueWealthHint(ctx, tx, state.HouseholdID, wealthHint)
		if e != nil {
			result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType}
			return result, true, nil
		}
		wealthID = id
	}
	if classification == "INVESTMENT_ACCOUNT" {
		wealthID = ""
	}
	// ADR-046: one shared classification operation for Web and Telegram; the
	// adapter keeps only its reply/evidence/telemetry side effects.
	transfer, err := reviewdomain.ClassifyTransferReview(ctx, tx, reviewdomain.TransferCommand{
		HouseholdID: state.HouseholdID, ActorUserID: userID, TransactionID: review.transactionID,
		ReviewItemID: pendingReviewItemID(ctx, tx, review.reviewID), RequestID: review.reviewID,
		Action: "TELEGRAM_TRANSFER_CLASSIFIED", Classification: classification,
		CategoryID: categoryID, WealthAccountID: wealthID,
	})
	if err != nil {
		if errors.Is(err, reviewdomain.ErrInvestmentAccountAmbiguous) {
			_ = tx.Rollback(ctx)
			if err := p.offerInvestmentChooser(ctx, state.SourceEventID, state.HouseholdID, review.reviewID, review.transactionID, state.Update); err != nil {
				return result, true, err
			}
			result.Status = "WEALTH_ACCOUNT_SELECTION_REQUIRED"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType}
			return result, true, nil
		}
		if errors.Is(err, reviewdomain.ErrWealthAccountRequired) {
			result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType}
			return result, true, nil
		}
		if errors.Is(err, reviewdomain.ErrWealthAccountIncompatible) {
			result.Status = "INCOMPATIBLE_WEALTH_ACCOUNT"
			return result, true, nil
		}
		if errors.Is(err, reviewdomain.ErrTransferNotFound) {
			return result, true, fmt.Errorf("review transaction no longer eligible")
		}
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, review.reviewID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification',$4::text)) ON CONFLICT DO NOTHING`, review.transactionID, state.SourceEventID, review.reviewID, classification); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CLASSIFY_TRANSFER','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'classification',$5::text,'type',$6::text,'purpose',$7::text,'related_wealth_account_id',NULLIF($8,'')::text,'status',$9::text,'agent_sprint',1))`, state.HouseholdID, userID, review.transactionID, review.reviewID, classification, transfer.Type, transfer.Purpose, transfer.WealthAccountID, transfer.Status); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "RESOLVED"
	result.Mutation = map[string]any{"action": "REVIEW_TRANSFER_CLASSIFIED", "classification": classification, "transaction_type": transfer.Type, "status": transfer.Status, "purpose": transfer.Purpose}
	return result, true, nil
}

func (p *Processor) agentResolveTransferCaseTx(ctx context.Context, state *agentState, caseID, target string, createNew bool) (string, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	valid, err := p.lockAgentReviewBindingTx(ctx, tx, state, state.ReviewBinding, "TRANSFER_RECONCILIATION")
	if err != nil {
		return "", err
	}
	if !valid || state.ReviewBinding.TargetID != caseID {
		return "", fmt.Errorf("stale transfer reconciliation binding")
	}
	var itemID, userID string
	if err := tx.QueryRow(ctx, `SELECT review_item_id::text FROM review_request WHERE id=$1 AND household_id=$2`, state.ReviewBinding.ReviewRequestID, state.HouseholdID).Scan(&itemID); err != nil {
		return "", err
	}
	if err := tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return "", err
	}
	action := "MERGE_EXISTING"
	if createNew {
		action = "CONFIRM_NEW_TRANSFER"
	}
	id, err := reviewdomain.ReconcileTransfer(ctx, tx, reviewdomain.TransferReconciliationCommand{
		HouseholdID: state.HouseholdID, ActorUserID: userID, ReviewItemID: itemID,
		CaseID: caseID, Action: action, CandidateID: target,
	})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1 AND household_id=$2`, state.SourceEventID, state.HouseholdID); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (p *Processor) agentResolveResidual(ctx context.Context, state *agentState, call gateway.ToolCall, action string, args map[string]any) (bool, agentToolResult, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	query := `SELECT r.id,ri.id,crc.id FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND r.review_type='CYCLE_RESIDUAL_ALLOCATION' AND r.status='OPEN' AND ri.status IN('PENDING_SEND','OPEN') AND rr.telegram_chat_id=$2 ORDER BY r.created_at DESC LIMIT 2`
	params := []any{state.HouseholdID, state.Update.Message.Chat.ID}
	if state.Update.Message.ReplyToMessage != nil {
		query = `SELECT r.id,ri.id,crc.id FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id JOIN review_request_recipient rr ON rr.review_request_id=r.id WHERE r.household_id=$1 AND r.review_type='CYCLE_RESIDUAL_ALLOCATION' AND r.status='OPEN' AND ri.status IN('PENDING_SEND','OPEN') AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3 LIMIT 2`
		params = append(params, state.Update.Message.ReplyToMessage.MessageID)
	}
	rows, err := p.pool.Query(ctx, query, params...)
	if err != nil {
		return true, result, err
	}
	defer rows.Close()
	type target struct{ request, item, caseID string }
	var targets []target
	for rows.Next() {
		var v target
		if err := rows.Scan(&v.request, &v.item, &v.caseID); err != nil {
			return true, result, err
		}
		targets = append(targets, v)
	}
	if len(targets) == 0 {
		return false, result, nil
	}
	if len(targets) != 1 {
		result.Status = "AMBIGUOUS_REVIEW"
		return true, result, nil
	}
	t := targets[0]
	if action == "TRANSACTION_MISSING" {
		if _, err := p.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
			return true, result, err
		}
		result.Status = "ACTION_REQUIRED"
		result.Mutation = map[string]any{"action": "ADD_MISSING_TRANSACTION_IN_TELEGRAM"}
		return true, result, nil
	}
	if action != "ALLOCATE_RETAINED_BALANCE" && action != "LEAVE_UNALLOCATED" {
		result.Status = "INVALID_REVIEW_ACTION"
		return true, result, nil
	}
	var input struct {
		Allocations []residualAllocation `json:"allocations"`
	}
	encoded, _ := json.Marshal(args)
	if err := json.Unmarshal(encoded, &input); err != nil {
		return true, result, err
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, result, err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return true, result, err
	}
	// ADR-046: cycle basis refresh, allocation validation, and completion live in
	// the shared operation; the agent lane maps the outcome to its own statuses.
	allocations := make([]reviewdomain.CycleAllocation, 0, len(input.Allocations))
	for _, allocation := range input.Allocations {
		allocations = append(allocations, reviewdomain.CycleAllocation{WealthAccountID: allocation.WealthAccountID, AmountIDR: allocation.AmountIDR, Note: allocation.Note})
	}
	outcome, err := reviewdomain.ApplyCycleResidual(ctx, tx, reviewdomain.CycleResidualCommand{
		HouseholdID: state.HouseholdID, CaseID: t.caseID, ReviewItemID: t.item, RequestID: t.request,
		Action: action, Allocations: allocations, ActorUserID: userID,
	})
	if err != nil {
		switch {
		case errors.Is(err, reviewdomain.ErrCycleAllocationsRequired):
			result.Status = "MISSING_ALLOCATIONS"
		case errors.Is(err, reviewdomain.ErrCycleAllocationInvalid):
			result.Status = "INVALID_ALLOCATION"
		case errors.Is(err, reviewdomain.ErrCycleAllocationDuplicate):
			result.Status = "DUPLICATE_WEALTH_ACCOUNT"
		case errors.Is(err, reviewdomain.ErrCycleAllocationMismatch):
			result.Status = "ALLOCATION_TOTAL_MISMATCH"
		case errors.Is(err, reviewdomain.ErrCycleWealthAccountInvalid):
			result.Status = "INVALID_WEALTH_ACCOUNT"
		default:
			return true, result, err
		}
		return true, result, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return true, result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,$3,'cycle_residual_case',$4,jsonb_build_object('residualIdr',$5,'agent_sprint',1))`, state.HouseholdID, userID, "CYCLE_RESIDUAL_"+action, t.caseID, outcome.Residual); err != nil {
		return true, result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return true, result, err
	}
	switch outcome.Outcome {
	case reviewdomain.CycleStaleNotApplicable:
		result.Status = "NO_LONGER_APPLICABLE"
		result.Mutation = map[string]any{"action": "CYCLE_RESIDUAL_CLOSED", "residual_idr": outcome.Residual}
		return true, result, nil
	case reviewdomain.CycleStaleRefreshed:
		result.Status = "STALE_REVIEW"
		result.Mutation = map[string]any{"action": "CYCLE_RESIDUAL_REFRESHED", "residual_idr": outcome.Residual}
		result.Review = map[string]any{"required": true, "review_type": "CYCLE_RESIDUAL_ALLOCATION"}
		return true, result, nil
	}
	result.Status = "RESOLVED"
	result.Mutation = map[string]any{"action": "CYCLE_RESIDUAL_RESOLVED", "resolution": action, "residual_idr": outcome.Residual}
	return true, result, nil
}
