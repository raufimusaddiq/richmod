// Bank fact completion (UIR-10). A bank email whose bounded verification could
// not confirm the extracted transaction semantics becomes a source-event review
// (UNKNOWN_BANK_TEMPLATE). Web queues COMPLETE_BANK_REVIEW; this operation owns
// the same account/household validation, missing-entity linkage, source refresh,
// and review completion so the Telegram reply lane and the worker job cannot
// diverge.
package reviewdomain

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// BankFactCommand carries one source-event review and the amount/date the user
// supplied. The account and canonical linkage are reloaded from the source, never
// trusted from the caller.
type BankFactCommand struct {
	HouseholdID   string
	UserID        string
	ReviewItemID  string
	SourceEventID string
	AccountID     string
	AmountIDR     string
	TransactionAt string
}

var (
	// ErrBankFactsRequired reports a completion missing the amount or time.
	ErrBankFactsRequired = errors.New("reviewdomain: bank amount and timestamp are required")
	// ErrBankReviewUnavailable reports a review that is not an open bank-fact item.
	ErrBankReviewUnavailable = errors.New("reviewdomain: bank review is unavailable")
	// ErrBankSourceUnlinked reports a bank source with no active account to bind.
	ErrBankSourceUnlinked = errors.New("reviewdomain: bank source has no active account")
)

// LinkBankSourceAccountAndComplete validates the review and account, marks the
// source linked, and completes the review. The caller performs the extraction
// re-evaluation in the same transaction.
func LinkBankSourceAccountAndComplete(ctx context.Context, tx pgx.Tx, cmd BankFactCommand) error {
	if strings.TrimSpace(cmd.AmountIDR) == "" || strings.TrimSpace(cmd.TransactionAt) == "" {
		return ErrBankFactsRequired
	}
	var listenerAccount string
	err := tx.QueryRow(ctx, `SELECT COALESCE(l.account_id::text,'') FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id JOIN bank_email_extraction e ON e.source_event_id=s.id JOIN bank_email_listener l ON l.id=e.listener_id WHERE ri.id=$1 AND ri.household_id=$2 AND ri.source_event_id=$3 AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.status IN ('OPEN','PENDING_SEND') FOR UPDATE OF ri`, cmd.ReviewItemID, cmd.HouseholdID, cmd.SourceEventID).Scan(&listenerAccount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBankReviewUnavailable
	}
	if err != nil {
		return err
	}
	if listenerAccount == "" {
		return ErrBankSourceUnlinked
	}
	if err := ValidateFinancialFundingAccount(ctx, tx, cmd.HouseholdID, listenerAccount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='bank-email-generic',parser_version='telegram-review' WHERE id=$1 AND household_id=$2`, cmd.SourceEventID, cmd.HouseholdID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=NULLIF($2,'')::uuid,resolution_action='COMPLETE_BANK_FACTS',resolution_values=jsonb_build_object('amount_idr',$3::text,'transaction_at',$4::text),updated_at=now() WHERE id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, cmd.UserID, cmd.AmountIDR, cmd.TransactionAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID); err != nil {
		return err
	}
	return nil
}
