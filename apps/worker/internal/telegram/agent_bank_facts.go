package telegram

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) agentResolveBoundBankFacts(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, binding *agentReviewBinding) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	action, _ := args["action"].(string)
	if action != "IGNORE" {
		result.Status = "UNSUPPORTED_REVIEW_ACTION"
		return result, true, nil
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var userID string
	err = tx.QueryRow(ctx, `SELECT ti.user_id::text
		FROM review_item ri
		JOIN review_request r ON r.review_item_id=ri.id AND r.id=$3::uuid
		JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=ri.household_id AND ti.active
		JOIN household_member hm ON hm.household_id=ri.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE ri.id=$1::uuid AND ri.household_id=$2 AND r.status='OPEN' AND ri.status IN ('OPEN','PENDING_SEND')
		  AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.transaction_id IS NULL
		FOR UPDATE OF ri`, binding.TargetID, state.HouseholdID, binding.ReviewRequestID, state.Update.Message.From.ID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	if err = reviewdomain.IgnoreBankReview(ctx, tx, reviewdomain.BankFactCommand{HouseholdID: state.HouseholdID, UserID: userID, ReviewItemID: binding.TargetID}, "TELEGRAM"); err != nil {
		if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) {
			result.Status = "STALE_REVIEW_BINDING"
			return result, true, nil
		}
		return result, true, err
	}
	result.Mutation = map[string]any{"action": "BANK_REVIEW_IGNORED"}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "RESOLVED"
	return result, true, nil
}
