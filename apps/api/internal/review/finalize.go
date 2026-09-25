package review

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// resolveTransactionReviewItem is the API adapter over the shared canonical
// resolve operation (ADR-046). Telegram calls the same package, so both surfaces
// lock, validate, and mutate the review through one implementation.
func resolveTransactionReviewItem(ctx context.Context, tx pgx.Tx, household, user, transactionID, action string) error {
	// The shared resolver records {"transaction_id": ...} by default because
	// MERGE_REVIEW telemetry joins on it.
	return reviewdomain.ResolveByTransaction(ctx, tx, reviewdomain.Command{
		HouseholdID: household,
		ActorUserID: user,
		SubjectID:   transactionID,
		Action:      action,
	})
}
