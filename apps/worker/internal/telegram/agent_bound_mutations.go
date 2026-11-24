package telegram

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) agentResolveBoundReview(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	if state.ReviewBinding == nil {
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect, Status: "NO_ACTIVE_REVIEW"}, true, nil
	}
	switch state.ReviewBinding.Kind {
	case "TRANSACTION":
		return p.agentResolveBoundTransactionReview(ctx, state, call, args, state.ReviewBinding)
	case "BANK_FACTS":
		return p.agentResolveBoundBankFacts(ctx, state, call, args, state.ReviewBinding)
	case "TRANSFER_RECONCILIATION":
		return p.agentResolveBoundTransferReconciliation(ctx, state, call, args, state.ReviewBinding)
	case "WEALTH_OBSERVATION":
		return p.agentResolveBoundWealthObservation(ctx, state, call, args, state.ReviewBinding)
	case "CYCLE_RESIDUAL":
		bound := *state
		if state.ReviewBinding.TelegramMessageID != 0 {
			bound.Update.Message.ReplyToMessage = &struct {
				MessageID int64 `json:"message_id"`
			}{MessageID: state.ReviewBinding.TelegramMessageID}
		}
		action, _ := args["action"].(string)
		handled, result, err := p.agentResolveResidual(ctx, &bound, call, action, args)
		if !handled && err == nil {
			result.Status = "STALE_REVIEW_BINDING"
		}
		return result, true, err
	default:
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect, Status: "STALE_REVIEW_BINDING"}, true, nil
	}
}

func (p *Processor) loadBoundTransactionReview(ctx context.Context, state *agentState, binding *agentReviewBinding) (*agentTransactionReview, error) {
	var review agentTransactionReview
	err := p.pool.QueryRow(ctx, `SELECT r.id,r.transaction_id,t.type,r.review_type,c.state,COALESCE(t.merchant_id::text,''),COALESCE(rr.telegram_message_id,0)
		FROM review_request r
		JOIN review_conversation c ON c.review_request_id=r.id
		JOIN transaction t ON t.id=r.transaction_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id AND rr.telegram_chat_id=$3
		WHERE r.id=$1 AND r.household_id=$2 AND r.status='OPEN' AND t.status='NEEDS_REVIEW'
		  AND ($4::bigint=0 OR rr.telegram_message_id=$4)
		LIMIT 1`, binding.ReviewRequestID, state.HouseholdID, state.Update.Message.Chat.ID, binding.TelegramMessageID).Scan(&review.reviewID, &review.transactionID,
		&review.transactionType, &review.reviewType, &review.conversationState, &review.merchantID, &review.messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &review, err
}

func (p *Processor) agentResolveBoundTransactionReview(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, binding *agentReviewBinding) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	review, err := p.loadBoundTransactionReview(ctx, state, binding)
	if err != nil {
		return result, true, err
	}
	if review == nil {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	var disputed bool
	if err = p.pool.QueryRow(ctx, `SELECT COALESCE(ri.decision->'decisionProvenance'->>'evidence_link_dispute','')='true' FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1`, review.reviewID).Scan(&disputed); err != nil {
		return result, true, err
	}
	if disputed {
		if action, _ := args["action"].(string); action != "IGNORE" {
			result.Status = "EVIDENCE_DISPUTE_USE_BUTTONS"
			return result, true, nil
		}
	}
	action, _ := args["action"].(string)
	categorySlug, _ := args["category_slug"].(string)
	description, _ := args["description"].(string)
	merchant, _ := args["merchant"].(string)
	payDate, _ := args["pay_date"].(string)
	amountIDR, _ := args["amount_idr"].(string)
	transactionAt, _ := args["transaction_at"].(string)
	wealthHint, _ := args["wealth_account_hint"].(string)

	switch action {
	case "PRIMARY_SALARY", "ORDINARY_INCOME":
		choice := "PRIMARY"
		if action == "ORDINARY_INCOME" {
			choice = "ORDINARY"
		}
		return p.agentResolveSalaryChoice(ctx, state, call, map[string]any{"choice": choice})
	case "IGNORE":
		var decision []byte
		if err = p.pool.QueryRow(ctx, `SELECT COALESCE(ri.decision,'{}'::jsonb) FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1 AND r.household_id=$2`, review.reviewID, state.HouseholdID).Scan(&decision); err != nil {
			return result, true, err
		}
		if !reviewdomain.IgnoreAllowed(review.reviewType, decision) {
			result.Status = "IGNORE_NOT_ALLOWED"
			return result, true, nil
		}
		return p.agentRejectTransactionReview(ctx, state, call, *review)
	case "OWN_ACCOUNT_TRANSFER":
		return p.agentResolveTransferClassification(ctx, state, call, *review, "OWN_ACCOUNT", "", "")
	case "HOUSEHOLD_TRANSFER":
		return p.agentResolveTransferClassification(ctx, state, call, *review, "HOUSEHOLD_ACCOUNT", "", "")
	case "INVESTMENT_TRANSFER":
		return p.agentResolveTransferClassification(ctx, state, call, *review, "INVESTMENT_ACCOUNT", "", "")
	case "ASSET_PURCHASE":
		if strings.TrimSpace(wealthHint) == "" {
			result.Status = "MISSING_WEALTH_ACCOUNT"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": []string{"wealth_account_hint"}}
			return result, true, nil
		}
		return p.agentResolveTransferClassification(ctx, state, call, *review, "ASSET_PURCHASE", wealthHint, "")
	case "EXPENSE":
		categoryID, err := p.agentCategoryID(ctx, state.HouseholdID, categorySlug)
		if err != nil || categoryID == "" {
			result.Status = "MISSING_CATEGORY"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": []string{"category_slug"}}
			return result, true, nil
		}
		return p.agentResolveTransferClassification(ctx, state, call, *review, "EXPENSE", "", categoryID)
	case "SET_PAY_DATE", "SET_TRANSACTION_DATE":
		if strings.TrimSpace(payDate) == "" {
			payDate = transactionAt
		}
		if !validReviewDate(payDate) {
			result.Status = "INVALID_PAY_DATE"
			result.Review = map[string]any{"required": true, "missing_fields": []string{"pay_date"}}
			return result, true, nil
		}
	case "COMPLETE_BANK_FACTS":
		if !slices.Contains(reviewdec.AgentActions(review.reviewType), "COMPLETE_BANK_FACTS") {
			result.Status = "UNSUPPORTED_REVIEW_ACTION"
			return result, true, nil
		}
		amount, ok := new(big.Int).SetString(amountIDR, 10)
		if strings.TrimSpace(amountIDR) == "" || !ok || amount.Sign() <= 0 || amount.String() != amountIDR || !validReviewTimestamp(transactionAt) {
			result.Status = "MISSING_BANK_FACTS"
			result.Review = map[string]any{"required": true, "missing_fields": []string{"amount_idr", "transaction_at"}}
			return result, true, nil
		}
		parsed, _ := time.Parse(time.RFC3339, transactionAt)
		applied, err := p.agentApplyBankFactsToTransaction(ctx, state, review.transactionID, amountIDR, parsed)
		if err != nil {
			return result, true, err
		}
		if !applied {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
	case "MERGE_EXISTING":
		candidateRef, _ := args["candidate_ref"].(string)
		return p.agentMergeDuplicateReview(ctx, state, call, *review, candidateRef)
	case "CONFIRM":
	default:
		result.Status = "UNSUPPORTED_REVIEW_ACTION"
		return result, true, nil
	}

	proposedDate := transactionAt
	for _, candidate := range []string{payDate, description} {
		if strings.TrimSpace(proposedDate) == "" {
			proposedDate = candidate
		}
	}
	if field, value, required := requiredNativeReviewDetail(review.reviewType, review.conversationState, review.merchantID, merchant, description, proposedDate); required {
		if strings.TrimSpace(value) == "" {
			result.Status = "MISSING_REVIEW_DETAIL"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": []string{field}}
			return result, true, nil
		}
		return p.agentSaveReviewField(ctx, state, call, *review, field, value)
	}
	categoryID := ""
	if strings.TrimSpace(categorySlug) != "" {
		categoryID, err = p.agentCategoryID(ctx, state.HouseholdID, categorySlug)
		if err != nil || categoryID == "" {
			result.Status = "INVALID_CATEGORY"
			return result, true, nil
		}
	}
	return p.agentConfirmTransactionReview(ctx, state, call, *review, categoryID, reviewExtraction{Description: clean(description, 500), Note: clean(merchant, 1000), PayDate: payDate, Confidence: 1})
}

func (p *Processor) agentApplyBankFactsToTransaction(ctx context.Context, state *agentState, transactionID, amountIDR string, at time.Time) (bool, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE transaction SET amount=$2,transaction_at=$3,updated_at=now() WHERE id=$1 AND household_id=$4 AND status='NEEDS_REVIEW'`, transactionID, amountIDR, at, state.HouseholdID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE transaction_proposal SET amount=$2,transaction_at=$3,updated_at=now() WHERE household_id=$4 AND id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')`, transactionID, amountIDR, at, state.HouseholdID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) SELECT $1,'TELEGRAM',ti.user_id,'COMPLETE_BANK_FACTS','transaction',$2,jsonb_build_object('amount_idr',$3::text,'transaction_at',$4::timestamptz,'agent_sprint',1) FROM telegram_identity ti WHERE ti.telegram_user_id=$5 AND ti.household_id=$1 AND ti.active`, state.HouseholdID, transactionID, amountIDR, at, state.Update.Message.From.ID); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Processor) agentResolveBoundTransferReconciliation(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, binding *agentReviewBinding) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	var originalSource, accountID, amount, description, purpose, wealthID string
	var at time.Time
	var candidates []string
	err := p.pool.QueryRow(ctx, `SELECT source_event_id,account_id::text,amount_idr::text,COALESCE(description,''),proposed_purpose,COALESCE(proposed_wealth_account_id::text,''),transaction_at,candidate_transaction_ids
		FROM transfer_reconciliation_case WHERE id=$1 AND household_id=$2 AND status='OPEN'`, binding.TargetID, state.HouseholdID).
		Scan(&originalSource, &accountID, &amount, &description, &purpose, &wealthID, &at, &candidates)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	action, _ := args["action"].(string)
	target, createNew := "", false
	switch action {
	case "MERGE_EXISTING":
		ref, _ := args["candidate_ref"].(string)
		var index int
		if _, err := fmt.Sscanf(ref, "candidate_%d", &index); err != nil || index < 1 || index > len(candidates) {
			result.Status = "INVALID_CANDIDATE"
			return result, true, nil
		}
		target = candidates[index-1]
	case "CONFIRM_NEW_TRANSFER":
		createNew = true
	case "IGNORE":
		return p.agentDismissBoundTransferReconciliation(ctx, state, call.CallID, binding)
	default:
		result.Status = "INVALID_REVIEW_ACTION"
		return result, true, nil
	}
	resolved, err := p.agentResolveTransferCaseTx(ctx, state, binding.TargetID, target, createNew)
	if err != nil {
		if strings.Contains(err.Error(), "stale transfer reconciliation binding") || errors.Is(err, reviewdomain.ErrTransferCaseUnavailable) {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
		return result, true, err
	}
	result.Status = "RESOLVED"
	result.Mutation = map[string]any{"action": "TRANSFER_RECONCILIATION_RESOLVED", "resolution": action, "amount_idr": amount, "purpose": purpose}
	if resolved != "" {
		refs, refErr := p.persistAgentTransactionReferences(ctx, state.HouseholdID, state.SourceEventID, state.Update, fmt.Sprintf("p%dr0", state.ModelPhases), []string{resolved})
		if refErr == nil {
			result.References = refs
		}
	}
	return result, true, nil
}

func (p *Processor) agentResolveBoundWealthObservation(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, binding *agentReviewBinding) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	action, _ := args["action"].(string)
	switch action {
	case "SET_WEALTH_ACCOUNT":
		wealthHint, _ := args["wealth_account_hint"].(string)
		tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return result, true, err
		}
		defer tx.Rollback(ctx)
		_, _, _, _, valid, err := p.loadBoundWealthObservationTx(ctx, tx, state, binding)
		if err != nil {
			return result, true, err
		}
		if !valid {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
		id, err := resolveUniqueWealthHint(ctx, tx, state.HouseholdID, wealthHint)
		if err != nil {
			result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
			return result, true, nil
		}
		err = reviewdomain.ResolveWealthObservation(ctx, tx, reviewdomain.WealthObservationCommand{HouseholdID: state.HouseholdID, ObservationID: binding.TargetID, WealthAccountID: id})
		if errors.Is(err, reviewdomain.ErrWealthObservationNotFound) {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
		if err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
			return result, true, err
		}
		if err = tx.Commit(ctx); err != nil {
			return result, true, err
		}
		result.Status = "UPDATED"
		result.Mutation = map[string]any{"action": "WEALTH_ACCOUNT_SET", "wealth_account_hint": wealthHint}
		return result, true, nil
	case "RECORD_ASSET_PURCHASE":
		return p.agentResolveBoundWealthAssetPurchase(ctx, state, call, args, binding)
	case "IGNORE":
		tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return result, true, err
		}
		defer tx.Rollback(ctx)
		_, _, _, originalSource, valid, err := p.loadBoundWealthObservationTx(ctx, tx, state, binding)
		if err != nil {
			return result, true, err
		}
		if !valid {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
		var userID string
		if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
			return result, true, err
		}
		err = reviewdomain.DismissWealthObservation(ctx, tx, reviewdomain.WealthObservationCommand{HouseholdID: state.HouseholdID, ObservationID: binding.TargetID, IgnoreFinancialEmail: true})
		if errors.Is(err, reviewdomain.ErrWealthObservationNotFound) {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
		if err != nil {
			return result, true, err
		}
		if err = resolveAgentBoundReviewRequestTx(ctx, tx, binding, userID, "IGNORED"); err != nil {
			return result, true, err
		}
		if originalSource != "" {
			if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, originalSource, state.HouseholdID); err != nil {
				return result, true, err
			}
		}
		if state.SourceEventID != originalSource {
			if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, state.SourceEventID, state.HouseholdID); err != nil {
				return result, true, err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return result, true, err
		}
		result.Status = "RESOLVED"
		result.Mutation = map[string]any{"action": "WEALTH_OBSERVATION_IGNORED"}
		return result, true, nil
	default:
		result.Status = "INVALID_REVIEW_ACTION"
		return result, true, nil
	}
}

func (p *Processor) agentResolveBoundWealthAssetPurchase(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, binding *agentReviewBinding) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	sourceHint, _ := args["source_account_hint"].(string)
	wealthHint, _ := args["wealth_account_hint"].(string)
	amount, _ := args["amount_idr"].(string)
	atText, _ := args["transaction_at"].(string)
	if strings.TrimSpace(sourceHint) == "" || !validReviewTimestamp(atText) {
		result.Status = "MISSING_ASSET_PURCHASE_FACTS"
		result.Review = map[string]any{"required": true, "missing_fields": []string{"source_account_hint", "transaction_at"}}
		return result, true, nil
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	_, institution, hint, originalSource, valid, err := p.loadBoundWealthObservationTx(ctx, tx, state, binding)
	if err != nil {
		return result, true, err
	}
	if !valid {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if strings.TrimSpace(wealthHint) == "" {
		wealthHint = strings.TrimSpace(institution + " " + hint)
	}
	if strings.TrimSpace(amount) == "" {
		if err = tx.QueryRow(ctx, `SELECT observed_value_idr::text FROM wealth_observation WHERE id=$1 AND household_id=$2 AND status='PENDING'`, binding.TargetID, state.HouseholdID).Scan(&amount); err != nil {
			return result, true, err
		}
	}
	parsed, _ := time.Parse(time.RFC3339, atText)
	local := parsed.In(jakartaLocation())
	transferArgs := map[string]any{"amount_idr": amount, "source_account_hint": sourceHint, "destination_wealth_account_hint": wealthHint, "reclassification_purpose": "ASSET_PURCHASE", "date_reference": "EXPLICIT", "explicit_date": local.Format("2006-01-02"), "local_time": local.Format("15:04"), "description": "Pembelian investasi dari bukti Telegram"}
	transferResult, _, err := p.agentRecordTransfer(ctx, state, gateway.ToolCall{CallID: call.CallID, Name: "record_transfer"}, transferArgs)
	if err != nil {
		return result, true, err
	}
	if transferResult.Status != "CONFIRMED" && transferResult.Status != "NO_OP_DUPLICATE" {
		transferResult.Tool = call.Name
		return transferResult, true, nil
	}
	var transactionID string
	if err = tx.QueryRow(ctx, `SELECT transaction_id::text FROM transaction_evidence WHERE source_event_id=$1 ORDER BY created_at DESC LIMIT 1`, state.SourceEventID).Scan(&transactionID); err != nil {
		return result, true, err
	}
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	if originalSource != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_IMAGE',1,jsonb_build_object('reclassified_from','WEALTH_OBSERVATION','observation_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, originalSource, binding.TargetID); err != nil {
			return result, true, err
		}
	}
	err = reviewdomain.DismissWealthObservation(ctx, tx, reviewdomain.WealthObservationCommand{HouseholdID: state.HouseholdID, ObservationID: binding.TargetID})
	if errors.Is(err, reviewdomain.ErrWealthObservationNotFound) {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	if err = reviewdomain.ReclassifyWealthEvidence(ctx, tx, state.HouseholdID, binding.TargetID); err != nil {
		return result, true, err
	}
	if err = resolveAgentBoundReviewRequestTx(ctx, tx, binding, userID, "RECLASSIFIED_ASSET_PURCHASE"); err != nil {
		return result, true, err
	}
	if originalSource != "" {
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, originalSource); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'RECLASSIFY_WEALTH_OBSERVATION','wealth_observation',$3,jsonb_build_object('transaction_id',$4::uuid,'purpose','ASSET_PURCHASE','agent_sprint',1))`, state.HouseholdID, userID, binding.TargetID, transactionID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result = transferResult
	result.Tool = call.Name
	result.Mutation["action"] = "WEALTH_OBSERVATION_RECORDED_AS_ASSET_PURCHASE"
	return result, true, nil
}
