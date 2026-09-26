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
	"time"

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

// ValidateBankSourceAccount checks that a bank-fact reply targets an open
// UNKNOWN_BANK_TEMPLATE item whose listener is bound to a live funding account,
// so the caller can fail fast (and re-prompt) before queueing completion. It
// deliberately does not resolve the review or touch the source: the shared
// COMPLETE_BANK_REVIEW job is the single owner of linking, persisting the
// transaction, and resolving the item, and would no-op against an
// already-resolved item.
func ValidateBankSourceAccount(ctx context.Context, tx pgx.Tx, cmd BankFactCommand) error {
	if err := ValidateBankFactValues(cmd.AmountIDR, cmd.TransactionAt); err != nil {
		return err
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
	return nil
}

// ValidateBankFactValues applies the same whole-IDR and zoned-time limits to
// Telegram and the queued bank completion before either can claim success.
func ValidateBankFactValues(amount, timestamp string) error {
	if strings.TrimSpace(amount) == "" || strings.TrimSpace(timestamp) == "" {
		return ErrBankFactsRequired
	}
	if !ValidBankAmountIDR(amount) {
		return ErrBankFactsRequired
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(timestamp)); err != nil {
		return ErrBankFactsRequired
	}
	return nil
}

func ValidBankAmountIDR(amount string) bool {
	return len(amount) > 0 && len(amount) <= 20 && strings.Trim(amount, "0123456789") == "" && strings.Trim(amount, "0") != ""
}
