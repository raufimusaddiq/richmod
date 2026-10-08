package telegram

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// agentResolveBoundBankFacts completes or dismisses a bank-email review whose
// facts could not be verified. The model extracts the amount and time from the
// household's answer; Go validates them and queues the same COMPLETE_BANK_REVIEW
// job the Web uses, which owns resolution and persistence.
func (p *Processor) agentResolveBoundBankFacts(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any, binding *agentReviewBinding) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolSideEffect}
	action, _ := args["action"].(string)
	amountIDR, _ := args["amount_idr"].(string)
	transactionAt, _ := args["transaction_at"].(string)
	if action == "COMPLETE_BANK_FACTS" {
		at, err := time.Parse(time.RFC3339, transactionAt)
		if reviewdomain.ValidateBankFactValues(amountIDR, transactionAt) != nil || err != nil {
			result.Status = "MISSING_BANK_FACTS"
			result.Review = map[string]any{"required": true, "missing_fields": []string{"amount_idr", "transaction_at"}, "format": "transaction_at as RFC3339 with offset"}
			return result, true, nil
		}
		transactionAt = at.Format(time.RFC3339)
	} else if action != "IGNORE" {
		result.Status = "UNSUPPORTED_REVIEW_ACTION"
		return result, true, nil
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	var bankSourceID, userID string
	err = tx.QueryRow(ctx, `SELECT ri.source_event_id::text,ti.user_id::text
		FROM review_item ri
		JOIN review_request r ON r.review_item_id=ri.id AND r.id=$3::uuid
		JOIN telegram_identity ti ON ti.telegram_user_id=$4 AND ti.household_id=ri.household_id AND ti.active
		JOIN household_member hm ON hm.household_id=ri.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE ri.id=$1::uuid AND ri.household_id=$2 AND r.status='OPEN' AND ri.status IN ('OPEN','PENDING_SEND')
		  AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.transaction_id IS NULL
		FOR UPDATE OF ri`, binding.TargetID, state.HouseholdID, binding.ReviewRequestID, state.Update.Message.From.ID).Scan(&bankSourceID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	if err != nil {
		return result, true, err
	}
	command := reviewdomain.BankFactCommand{HouseholdID: state.HouseholdID, UserID: userID, ReviewItemID: binding.TargetID, SourceEventID: bankSourceID, AmountIDR: amountIDR, TransactionAt: transactionAt}

	if action == "IGNORE" {
		if err = reviewdomain.IgnoreBankReview(ctx, tx, command, "TELEGRAM"); err != nil {
			if errors.Is(err, reviewdomain.ErrBankReviewUnavailable) {
				result.Status = "STALE_REVIEW_BINDING"
				return result, true, nil
			}
			return result, true, err
		}
		result.Mutation = map[string]any{"action": "BANK_REVIEW_IGNORED"}
	} else {
		if err = enqueueBankFactsCompletion(ctx, tx, command, state.Update.Message.Chat.ID); err != nil {
			switch {
			case errors.Is(err, reviewdomain.ErrBankReviewUnavailable), errors.Is(err, reviewdomain.ErrAlreadyResolved):
				result.Status = "STALE_REVIEW_BINDING"
				return result, true, nil
			case errors.Is(err, reviewdomain.ErrBankSourceUnlinked):
				accounts, listErr := reviewdomain.ListBankSourceAccountChoices(ctx, tx, state.HouseholdID, binding.TargetID, bankSourceID, 10)
				if listErr != nil {
					return result, true, listErr
				}
				_ = tx.Rollback(ctx)
				if len(accounts) == 0 {
					result.Status = "BANK_ACCOUNT_UNAVAILABLE"
					return result, true, nil
				}
				// Keep the facts on the review and ask which account the email is for;
				// the account button completes it through the existing callback lane.
				if err = p.offerBankAccountChooser(ctx, state.SourceEventID, state.HouseholdID, binding.TargetID, accounts, amountIDR, transactionAt, state.Update); err != nil {
					return result, true, err
				}
				result.Status = "BANK_ACCOUNT_REQUIRED"
				return result, true, nil
			}
			return result, true, err
		}
		result.Mutation = map[string]any{"action": "BANK_FACTS_QUEUED", "amount_idr": amountIDR}
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1`, state.SourceEventID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "RESOLVED"
	if action == "COMPLETE_BANK_FACTS" {
		// The job resolves the review; until then it is accepted, not recorded.
		result.Status = "QUEUED"
	}
	return result, true, nil
}
