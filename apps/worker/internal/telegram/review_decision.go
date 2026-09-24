package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

func telegramReviewDecision(ctx context.Context, tx pgx.Tx, transactionID, reviewType string) (reviewdec.Decision, error) {
	decision, ok := reviewdec.Preset(reviewType, "transaction", transactionID)
	if !ok {
		return reviewdec.Decision{}, fmt.Errorf("no review decision preset for %s", reviewType)
	}
	var amount, sourceEventID string
	var transactionAt time.Time
	var transactionType string
	if err := tx.QueryRow(ctx, "SELECT COALESCE(amount::text,''),transaction_at,COALESCE(source_event_id::text,''),type FROM transaction WHERE id=$1::uuid", transactionID).
		Scan(&amount, &transactionAt, &sourceEventID, &transactionType); err != nil {
		return reviewdec.Decision{}, err
	}
	if amount != "" {
		decision.KnownFacts["amount_idr"] = amount
	}
	if !transactionAt.IsZero() {
		decision.KnownFacts["transaction_at"] = transactionAt.Format(time.RFC3339)
	}
	if transactionType != "" {
		decision.KnownFacts["type"] = transactionType
	}
	decision.SourceEventID = sourceEventID
	return decision, nil
}

// insertSourceEventReviewDecision writes the shared PRD §7 contract with the
// source-event review. Unknown reasons fail closed instead of storing a zero
// decision.
func insertSourceEventReviewDecision(ctx context.Context, tx pgx.Tx, household, source, reason string, known, provenance map[string]any) error {
	decision, ok := reviewdec.Preset(reason, "source_event", source)
	if !ok {
		return fmt.Errorf("no review decision preset for %s", reason)
	}
	for key, value := range known {
		decision.KnownFacts[key] = value
	}
	for key, value := range provenance {
		decision.Provenance[key] = value
	}
	encoded, err := decision.JSON()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) SELECT $1,$2,$3,'OPEN',$4::jsonb WHERE NOT EXISTS (SELECT 1 FROM review_item WHERE source_event_id=$2 AND status IN ('PENDING_SEND','OPEN'))", household, source, reason, string(encoded))
	return err
}
