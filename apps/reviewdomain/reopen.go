package reviewdomain

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// possibleDuplicateDecision is the ReviewDecision contract for a transaction whose
// duplicate relationship is undecided. It mirrors the worker's POSSIBLE_DUPLICATE
// preset (apps/worker/internal/reviewdec), which apps/api cannot import.
func possibleDuplicateDecision(transactionID string, known map[string]any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"version": 1, "subject": map[string]any{"type": "transaction", "id": transactionID},
		"reasonCode": "POSSIBLE_DUPLICATE", "decisionClass": "DUPLICATE_AMBIGUITY", "decisionSource": "DETERMINISTIC",
		"knownFacts": known, "missingFacts": []string{"duplicate_relationship"},
		"allowedActions":    []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"},
		"interactionMode":   "BOUNDED_CHOICE",
		"evidenceRefs":      []map[string]string{{"kind": "transaction", "id": transactionID}},
		"whyNotAutoConfirm": "a plausible duplicate exists; choose the matching event, confirm as new, or ignore",
	})
}

// ReopenTransactionReview gives a transaction that a reversal returned to
// NEEDS_REVIEW its canonical active review_item again. Resolved items are
// history and stay untouched; a new OPEN item carries the most recent complete
// ReviewDecision the household answered for this transaction. A transaction
// whose only earlier items predate the decision contract reopens as
// POSSIBLE_DUPLICATE: reversing a merge makes the duplicate relationship
// undecided again. Idempotent: an existing active item is kept as is.
func ReopenTransactionReview(ctx context.Context, tx pgx.Tx, household, transactionID string) error {
	var amount, at string
	if err := tx.QueryRow(ctx, `SELECT amount::text,transaction_at::text FROM transaction WHERE id=$1 AND household_id=$2`, transactionID, household).Scan(&amount, &at); err != nil {
		return err
	}
	fallback, err := possibleDuplicateDecision(transactionID, map[string]any{"amount_idr": amount, "transaction_at": at})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision,preferred_user_id)
		SELECT t.household_id,t.id,COALESCE(prev.review_type,'POSSIBLE_DUPLICATE'),'OPEN',COALESCE(prev.decision,$3::jsonb),t.created_by_user_id
		FROM transaction t
		LEFT JOIN LATERAL (
			SELECT ri.review_type,ri.decision FROM review_item ri
			WHERE ri.transaction_id=t.id AND COALESCE(ri.decision->>'reasonCode','')<>''
			AND jsonb_typeof(ri.decision->'allowedActions')='array' AND jsonb_array_length(ri.decision->'allowedActions')>0
			ORDER BY ri.created_at DESC LIMIT 1
		) prev ON true
		WHERE t.id=$1 AND t.household_id=$2 AND t.status='NEEDS_REVIEW'
		AND NOT EXISTS (SELECT 1 FROM review_item WHERE transaction_id=t.id AND status IN ('OPEN','PENDING_SEND'))`, transactionID, household, string(fallback))
	return err
}
