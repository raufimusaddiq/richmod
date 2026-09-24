package telegram

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// insertSourceEventReviewDecision opens a source-event-scoped review together
// with its canonical ReviewDecision contract (PRD 7, 37), so the Inbox can
// always explain why the household's input is required. Idempotent on an open
// review; a reason with no preset is a programming error, not a silent skip.
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
	_, err = tx.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) SELECT $1,$2,$3,'OPEN',$4::jsonb WHERE NOT EXISTS (SELECT 1 FROM review_item WHERE source_event_id=$2 AND status IN ('PENDING_SEND','OPEN'))`, household, source, reason, string(encoded))
	return err
}
