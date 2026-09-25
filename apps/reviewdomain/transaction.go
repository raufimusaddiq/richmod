package reviewdomain

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidCandidate reports a resolution that referenced a candidate no longer
// valid for this household (stale callback, cross-household ID, inactive row).
var ErrInvalidCandidate = errors.New("reviewdomain: invalid candidate")

// ValidateCategoryForHousehold confirms a selected category is active and owned
// by the household. Both Web and Telegram call this before mutating a
// transaction review, so callback payloads never trust stale canonical IDs.
func ValidateCategoryForHousehold(ctx context.Context, tx pgx.Tx, household, categoryID string) error {
	if categoryID == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM category WHERE id=$1 AND household_id=$2 AND active)", categoryID, household).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrInvalidCandidate
	}
	return nil
}

// ValidateTransactionReview locks the review subject and revalidates the
// household binding. It returns ErrAlreadyResolved when the transaction is no
// longer open, so adapters can answer a stale action deterministically instead
// of replaying a canonical mutation (ADR-046).
func ValidateTransactionReview(ctx context.Context, tx pgx.Tx, household, transactionID string) error {
	var id string
	err := tx.QueryRow(ctx, "SELECT id FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE", transactionID, household).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	return err
}
