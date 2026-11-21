package reviewdomain

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrMergeTargetActive = errors.New("reviewdomain: transaction is the target of an active merge")

const mergeTargetConstraint = "transaction_active_merge_target"

func MergeTargetError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == mergeTargetConstraint {
		return ErrMergeTargetActive
	}
	return err
}

func ActiveMergeTarget(ctx context.Context, tx pgx.Tx, householdID, transactionID string) (bool, error) {
	var protected bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reconciliation_merge m WHERE m.household_id=$1 AND m.target_transaction_id=$2 AND m.status='ACTIVE')
		AND NOT EXISTS(SELECT 1 FROM reconciliation_merge s WHERE s.household_id=$1 AND s.source_transaction_id=$2 AND s.status='ACTIVE')`, householdID, transactionID).Scan(&protected)
	return protected, err
}
