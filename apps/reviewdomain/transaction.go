package reviewdomain

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidCandidate = errors.New("reviewdomain: invalid candidate")

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

func ValidateTransactionReview(ctx context.Context, tx pgx.Tx, household, transactionID string) error {
	var id string
	err := tx.QueryRow(ctx, "SELECT id FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE", transactionID, household).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	return err
}
