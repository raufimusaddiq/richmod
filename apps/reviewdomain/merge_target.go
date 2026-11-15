package reviewdomain

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrMergeTargetActive reports an attempt to void a transaction that is the target of an
// ACTIVE reconciliation merge. Voiding it would leave the merged real event with no live
// transaction, so the household must reverse the merge first. Migration 00100 enforces it
// in the database; callers map this error to a refusal instead of a server error.
var ErrMergeTargetActive = errors.New("reviewdomain: transaction is the target of an active merge")

const mergeTargetConstraint = "transaction_active_merge_target"

// MergeTargetError converts the database guard's error into ErrMergeTargetActive and
// returns any other error unchanged. The failed statement has aborted the Postgres
// transaction, so the caller must roll back.
func MergeTargetError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == mergeTargetConstraint {
		return ErrMergeTargetActive
	}
	return err
}

// ActiveMergeTarget reports whether the transaction is the target of an ACTIVE merge, so a
// caller can refuse cleanly before writing. A transaction that is itself the source of an
// ACTIVE merge (chained merge) is not protected; its event continues in that merge's target.
func ActiveMergeTarget(ctx context.Context, tx pgx.Tx, householdID, transactionID string) (bool, error) {
	var protected bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reconciliation_merge m WHERE m.household_id=$1 AND m.target_transaction_id=$2 AND m.status='ACTIVE')
		AND NOT EXISTS(SELECT 1 FROM reconciliation_merge s WHERE s.household_id=$1 AND s.source_transaction_id=$2 AND s.status='ACTIVE')`, householdID, transactionID).Scan(&protected)
	return protected, err
}
