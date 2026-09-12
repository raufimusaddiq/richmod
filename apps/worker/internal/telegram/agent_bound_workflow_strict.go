package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// agentResolveBoundResidualWithHints keeps Wealth Account UUIDs server-private.
// The model supplies human-readable hints; Go resolves them and passes only the
// resolved canonical IDs into the existing deterministic residual allocator.
func (p *Processor) agentResolveBoundResidualWithHints(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	binding := state.ReviewBinding
	if binding == nil || binding.Kind != "CYCLE_RESIDUAL" || binding.TargetID == "" || binding.ReviewRequestID == "" {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	// Residual mutation must be tied to the concrete Telegram review message.
	// An unsent/unbound review is not actionable from free text.
	if binding.TelegramMessageID == 0 {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}

	action, _ := args["action"].(string)
	translated := make(map[string]any, len(args))
	for key, value := range args {
		translated[key] = value
	}
	if action == "ALLOCATE_RETAINED_BALANCE" {
		raw, ok := args["allocations"].([]any)
		if !ok || len(raw) == 0 {
			result.Status = "MISSING_ALLOCATIONS"
			return result, true, nil
		}
		tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return result, true, err
		}
		defer tx.Rollback(ctx)
		resolved := make([]map[string]any, 0, len(raw))
		seen := map[string]struct{}{}
		for _, rawAllocation := range raw {
			allocation, ok := rawAllocation.(map[string]any)
			if !ok {
				return result, true, fmt.Errorf("invalid residual allocation")
			}
			hint, _ := allocation["wealth_account_hint"].(string)
			amount, _ := allocation["amount_idr"].(string)
			note, _ := allocation["note"].(string)
			if strings.TrimSpace(hint) == "" {
				result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
				return result, true, nil
			}
			id, resolveErr := resolveUniqueWealthHint(ctx, tx, state.HouseholdID, hint)
			if resolveErr != nil {
				result.Status = "WEALTH_ACCOUNT_AMBIGUOUS"
				result.Facts = map[string]any{"wealth_account_hint": clean(hint, 160)}
				return result, true, nil
			}
			if _, duplicate := seen[id]; duplicate {
				result.Status = "DUPLICATE_WEALTH_ACCOUNT"
				return result, true, nil
			}
			seen[id] = struct{}{}
			resolved = append(resolved, map[string]any{"wealth_account_id": id, "amount_idr": amount, "note": note})
		}
		translated["allocations"] = resolved
	}

	bound := *state
	bound.Update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: binding.TelegramMessageID}
	handled, result, err := p.agentResolveResidual(ctx, &bound, call, action, translated)
	if !handled && err == nil {
		result.Status = "STALE_REVIEW_BINDING"
	}
	return result, true, err
}

func (p *Processor) agentResolveBoundMerchantLearningStrict(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	binding := state.MerchantLearningBinding
	if binding == nil || binding.ReviewRequestID == "" || binding.TransactionID == "" {
		result.Status = "NO_MERCHANT_LEARNING_PENDING"
		return result, true, nil
	}
	remember, _ := args["remember"].(bool)

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)

	var lockedReview string
	err = tx.QueryRow(ctx, `
		SELECT r.id::text
		FROM review_request r
		JOIN review_conversation c ON c.review_request_id=r.id
		JOIN transaction t ON t.id=r.transaction_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		WHERE r.id=$1 AND r.household_id=$2 AND r.transaction_id=$3
		  AND r.status='OPEN' AND c.state='AWAITING_CONFIRMATION' AND t.status='CONFIRMED'
		  AND rr.telegram_chat_id=$4 AND ($5::bigint=0 OR rr.telegram_message_id=$5)
		FOR UPDATE OF r,c,t,rr`, binding.ReviewRequestID, state.HouseholdID, binding.TransactionID, state.Update.Message.Chat.ID, binding.TelegramMessageID).Scan(&lockedReview)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "STALE_MERCHANT_LEARNING_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}

	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	merchantName, categoryName := binding.Merchant, binding.Category
	if remember {
		var merchantID, categoryID string
		if err = tx.QueryRow(ctx, `SELECT t.merchant_id,t.category_id,m.normalized_name,c.name FROM transaction t JOIN merchant m ON m.id=t.merchant_id JOIN category c ON c.id=t.category_id WHERE t.id=$1 AND t.household_id=$2 AND t.status='CONFIRMED' AND t.merchant_id IS NOT NULL AND t.category_id IS NOT NULL`, binding.TransactionID, state.HouseholdID).Scan(&merchantID, &categoryID, &merchantName, &categoryName); err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 AND household_id=$1 ON CONFLICT(household_id,raw_name) DO UPDATE SET default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true`, state.HouseholdID, merchantID, categoryID); err != nil {
			return result, true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'REMEMBER_MERCHANT','transaction',$3,jsonb_build_object('review_request_id',$4::uuid,'merchant_id',$5::uuid,'category_id',$6::uuid,'agent_sprint',1))`, state.HouseholdID, userID, binding.TransactionID, binding.ReviewRequestID, merchantID, categoryID); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1 AND household_id=$2 AND status='OPEN'`, binding.ReviewRequestID, state.HouseholdID); err != nil {
		return result, true, err
	}
	if err = resolveCanonicalReviewItem(ctx, tx, binding.ReviewRequestID, "TELEGRAM_MERCHANT_DECISION"); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, binding.ReviewRequestID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($3,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$1::uuid,'remember_merchant',$4::boolean)) ON CONFLICT DO NOTHING`, binding.ReviewRequestID, state.SourceEventID, binding.TransactionID, remember); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "CONFIRMED"
	result.Mutation = map[string]any{"action": "MERCHANT_LEARNING_RESOLVED", "remember": remember, "merchant": merchantName, "category": categoryName}
	return result, true, nil
}
