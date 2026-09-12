package telegram

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) executeAgentSpecializedSideEffectBound(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	switch call.Name {
	case "resolve_review":
		return p.agentResolveBoundReview(ctx, state, call, args)
	case "resolve_merchant_learning":
		return p.agentResolveBoundMerchantLearning(ctx, state, call, args)
	case "record_transfer", "resolve_salary_choice":
		return p.executeAgentSpecializedSideEffect(ctx, state, call, args)
	default:
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}, true, fmt.Errorf("unsupported specialized side effect %q", call.Name)
	}
}

func (p *Processor) agentResolveBoundReview(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	if state.ReviewBinding == nil {
		return agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect, Status: "NO_ACTIVE_REVIEW"}, true, nil
	}
	switch state.ReviewBinding.Kind {
	case "TRANSACTION":
		return p.agentResolveBoundTransactionReview(ctx, state, call, args, state.ReviewBinding)
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
		LEFT JOIN review_request_recipient rr ON rr.review_request_id=r.id AND rr.telegram_chat_id=$3
		WHERE r.id=$1 AND r.household_id=$2 AND r.status='OPEN' AND t.status='NEEDS_REVIEW'
		LIMIT 1`, binding.ReviewRequestID, state.HouseholdID, state.Update.Message.Chat.ID).Scan(&review.reviewID, &review.transactionID,
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
		return p.agentRejectTransactionReview(ctx, state, call, *review)
	case "OWN_ACCOUNT_TRANSFER":
		return p.agentResolveTransferClassification(ctx, state, call, *review, "TRANSFER", "CONFIRMED", "OWN_ACCOUNT", "", "")
	case "HOUSEHOLD_TRANSFER":
		return p.agentResolveTransferClassification(ctx, state, call, *review, "TRANSFER", "CONFIRMED", "HOUSEHOLD_ACCOUNT", "", "")
	case "INVESTMENT_TRANSFER":
		return p.agentResolveTransferClassification(ctx, state, call, *review, "TRANSFER", "CONFIRMED", "INVESTMENT_ACCOUNT", "", "")
	case "ASSET_PURCHASE":
		if strings.TrimSpace(wealthHint) == "" {
			result.Status = "MISSING_WEALTH_ACCOUNT"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": []string{"wealth_account_hint"}}
			return result, true, nil
		}
		return p.agentResolveTransferClassification(ctx, state, call, *review, "TRANSFER", "CONFIRMED", "ASSET_PURCHASE", wealthHint, "")
	case "EXPENSE":
		categoryID, err := p.agentCategoryID(ctx, state.HouseholdID, categorySlug)
		if err != nil || categoryID == "" {
			result.Status = "MISSING_CATEGORY"
			result.Review = map[string]any{"required": true, "review_type": review.reviewType, "missing_fields": []string{"category_slug"}}
			return result, true, nil
		}
		return p.agentResolveTransferClassification(ctx, state, call, *review, "EXPENSE", "CONFIRMED", "EXPENSE", "", categoryID)
	case "SET_PAY_DATE":
		if !validReviewDate(payDate) {
			result.Status = "INVALID_PAY_DATE"
			result.Review = map[string]any{"required": true, "missing_fields": []string{"pay_date"}}
			return result, true, nil
		}
	case "COMPLETE_BANK_FACTS":
		amount, ok := new(big.Int).SetString(amountIDR, 10)
		if strings.TrimSpace(amountIDR) == "" || !ok || amount.Sign() <= 0 || amount.String() != amountIDR || !validReviewTimestamp(transactionAt) {
			result.Status = "MISSING_BANK_FACTS"
			result.Review = map[string]any{"required": true, "missing_fields": []string{"amount_idr", "transaction_at"}}
			return result, true, nil
		}
		parsed, _ := time.Parse(time.RFC3339, transactionAt)
		if _, err := p.pool.Exec(ctx, `UPDATE transaction SET amount=$2,transaction_at=$3,updated_at=now() WHERE id=$1 AND household_id=$4 AND status='NEEDS_REVIEW'`, review.transactionID, amountIDR, parsed, state.HouseholdID); err != nil {
			return result, true, err
		}
	case "CONFIRM":
	default:
		result.Status = "UNSUPPORTED_REVIEW_ACTION"
		return result, true, nil
	}

	if field, value, required := requiredNativeReviewDetail(review.reviewType, review.conversationState, review.merchantID, merchant, description); required {
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
	default:
		result.Status = "INVALID_REVIEW_ACTION"
		return result, true, nil
	}
	resolved, err := p.agentResolveTransferCaseTx(ctx, state, binding.TargetID, originalSource, accountID, amount, description, purpose, wealthID, at, target, createNew)
	if err != nil {
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
	var resolved, institution, hint, originalSource string
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(wo.resolved_wealth_account_id::text,''),wo.institution,wo.account_hint,d.source_event_id
		FROM wealth_observation wo JOIN review_item ri ON ri.wealth_observation_id=wo.id JOIN document d ON d.id=wo.document_id
		WHERE wo.id=$1 AND wo.household_id=$2 AND wo.status='PENDING' AND ri.status IN ('OPEN','PENDING_SEND') LIMIT 1`, binding.TargetID, state.HouseholdID).
		Scan(&resolved, &institution, &hint, &originalSource)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	action, _ := args["action"].(string)
	switch action {
	case "PREPARE_SNAPSHOT":
		if resolved == "" {
			result.Status = "MISSING_WEALTH_ACCOUNT"
			result.Review = map[string]any{"required": true, "review_type": "WEALTH_OBSERVATION", "missing_fields": []string{"wealth_account_hint"}}
			return result, true, nil
		}
		if _, err := p.pool.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
			return result, true, err
		}
		result.Status = "ACTION_REQUIRED"
		result.Mutation = map[string]any{"action": "PREPARE_WEALTH_SNAPSHOT", "requires_web": true}
		return result, true, nil
	case "SET_WEALTH_ACCOUNT":
		wealthHint, _ := args["wealth_account_hint"].(string)
		tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil { return result, true, err }
		defer tx.Rollback(ctx)
		id, err := resolveUniqueWealthHint(ctx, tx, state.HouseholdID, wealthHint)
		if err != nil {
			result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
			return result, true, nil
		}
		if _, err = tx.Exec(ctx, `UPDATE wealth_observation SET resolved_wealth_account_id=$2,updated_at=now() WHERE id=$1 AND household_id=$3 AND status='PENDING'`, binding.TargetID, id, state.HouseholdID); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil { return result,true,err }
		if err = tx.Commit(ctx); err != nil { return result,true,err }
		result.Status = "UPDATED"
		result.Mutation = map[string]any{"action": "WEALTH_ACCOUNT_SET", "wealth_account_hint": wealthHint}
		return result, true, nil
	case "RECORD_ASSET_PURCHASE":
		return p.agentResolveBoundWealthAssetPurchase(ctx, state, call, args, binding, institution, hint, originalSource)
	case "IGNORE":
		tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil { return result,true,err }
		defer tx.Rollback(ctx)
		var userID string
		if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `UPDATE wealth_observation SET status='DISMISSED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status='PENDING'`, binding.TargetID, state.HouseholdID); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='IGNORED',updated_at=now() WHERE wealth_observation_id=$1 AND status IN ('OPEN','PENDING_SEND')`, binding.TargetID, userID); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id IN($1,$2)`, originalSource, state.SourceEventID); err != nil { return result,true,err }
		if err = tx.Commit(ctx); err != nil { return result,true,err }
		result.Status = "RESOLVED"
		result.Mutation = map[string]any{"action": "WEALTH_OBSERVATION_IGNORED"}
		return result, true, nil
	default:
		result.Status = "INVALID_REVIEW_ACTION"
		return result, true, nil
	}
}

func (p *Processor) agentResolveBoundWealthAssetPurchase(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, binding *agentReviewBinding, institution, hint, originalSource string) (agentToolResult, bool, error) {
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
	if strings.TrimSpace(wealthHint) == "" {
		wealthHint = strings.TrimSpace(institution + " " + hint)
	}
	if strings.TrimSpace(amount) == "" {
		if err := p.pool.QueryRow(ctx, `SELECT observed_value_idr::text FROM wealth_observation WHERE id=$1 AND household_id=$2 AND status='PENDING'`, binding.TargetID, state.HouseholdID).Scan(&amount); err != nil {
			return result, true, err
		}
	}
	parsed, _ := time.Parse(time.RFC3339, atText)
	local := parsed.In(jakartaLocation())
	transferArgs := map[string]any{"amount_idr": amount, "source_account_hint": sourceHint, "destination_wealth_account_hint": wealthHint, "purpose": "ASSET_PURCHASE", "date_reference": "EXPLICIT", "explicit_date": local.Format("2006-01-02"), "local_time": local.Format("15:04"), "description": "Pembelian investasi dari bukti Telegram"}
	transferResult, _, err := p.agentRecordTransfer(ctx, state, gateway.ToolCall{CallID: call.CallID, Name: "record_transfer"}, transferArgs)
	if err != nil { return result,true,err }
	if transferResult.Status != "CONFIRMED" && transferResult.Status != "NO_OP_DUPLICATE" {
		transferResult.Tool = call.Name
		return transferResult, true, nil
	}
	var transactionID string
	if err := p.pool.QueryRow(ctx, `SELECT transaction_id::text FROM transaction_evidence WHERE source_event_id=$1 ORDER BY created_at DESC LIMIT 1`, state.SourceEventID).Scan(&transactionID); err != nil { return result,true,err }
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return result,true,err }
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_IMAGE',1,jsonb_build_object('reclassified_from','WEALTH_OBSERVATION','observation_id',$3::uuid)) ON CONFLICT DO NOTHING`, transactionID, originalSource, binding.TargetID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE wealth_observation SET status='DISMISSED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status='PENDING'`, binding.TargetID, state.HouseholdID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE document SET document_type='TRANSACTION_HISTORY_SCREENSHOT',status='EXTRACTED',updated_at=now() WHERE id=(SELECT document_id FROM wealth_observation WHERE id=$1)`, binding.TargetID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='RECLASSIFIED_ASSET_PURCHASE',updated_at=now() WHERE wealth_observation_id=$1 AND status IN ('OPEN','PENDING_SEND')`, binding.TargetID, userID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, originalSource); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'RECLASSIFY_WEALTH_OBSERVATION','wealth_observation',$3,jsonb_build_object('transaction_id',$4::uuid,'purpose','ASSET_PURCHASE','agent_sprint',1))`, state.HouseholdID, userID, binding.TargetID, transactionID); err != nil { return result,true,err }
	if err = tx.Commit(ctx); err != nil { return result,true,err }
	result = transferResult
	result.Tool = call.Name
	result.Mutation["action"] = "WEALTH_OBSERVATION_RECORDED_AS_ASSET_PURCHASE"
	return result, true, nil
}

func (p *Processor) agentResolveBoundMerchantLearning(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	if state.MerchantLearningBinding == nil {
		result.Status = "NO_MERCHANT_LEARNING_PENDING"
		return result, true, nil
	}
	binding := state.MerchantLearningBinding
	remember, _ := args["remember"].(bool)
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return result,true,err }
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil { return result,true,err }
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_request r JOIN review_conversation c ON c.review_request_id=r.id JOIN transaction t ON t.id=r.transaction_id WHERE r.id=$1 AND r.household_id=$2 AND r.transaction_id=$3 AND r.status='OPEN' AND c.state='AWAITING_CONFIRMATION' AND t.status='CONFIRMED')`, binding.ReviewRequestID, state.HouseholdID, binding.TransactionID).Scan(&active); err != nil { return result,true,err }
	if !active {
		result.Status = "STALE_MERCHANT_LEARNING_BINDING"
		return result, true, nil
	}
	merchantName, categoryName := binding.Merchant, binding.Category
	if remember {
		var merchantID, categoryID string
		if err = tx.QueryRow(ctx, `SELECT t.merchant_id,t.category_id,m.normalized_name,c.name FROM transaction t JOIN merchant m ON m.id=t.merchant_id JOIN category c ON c.id=t.category_id WHERE t.id=$1 AND t.household_id=$2 AND t.status='CONFIRMED' AND t.merchant_id IS NOT NULL AND t.category_id IS NOT NULL`, binding.TransactionID, state.HouseholdID).Scan(&merchantID, &categoryID, &merchantName, &categoryName); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 AND household_id=$1 ON CONFLICT(household_id,raw_name) DO UPDATE SET default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true`, state.HouseholdID, merchantID, categoryID); err != nil { return result,true,err }
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'REMEMBER_MERCHANT','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'merchant_id',$5::uuid,'category_id',$6::uuid,'agent_sprint',1))`, state.HouseholdID, userID, binding.TransactionID, binding.ReviewRequestID, merchantID, categoryID); err != nil { return result,true,err }
	}
	if _, err = tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1 AND household_id=$2 AND status='OPEN'`, binding.ReviewRequestID, state.HouseholdID); err != nil { return result,true,err }
	if err = resolveCanonicalReviewItem(ctx, tx, binding.ReviewRequestID, "TELEGRAM_MERCHANT_DECISION"); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, binding.ReviewRequestID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil { return result,true,err }
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($3,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$1::uuid,'remember_merchant',$4::boolean)) ON CONFLICT DO NOTHING`, binding.ReviewRequestID, state.SourceEventID, binding.TransactionID, remember); err != nil { return result,true,err }
	if err = tx.Commit(ctx); err != nil { return result,true,err }
	result.Status = "CONFIRMED"
	result.Mutation = map[string]any{"action": "MERCHANT_LEARNING_RESOLVED", "remember": remember, "merchant": merchantName, "category": categoryName}
	return result, true, nil
}
