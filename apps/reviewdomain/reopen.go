package reviewdomain

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// possibleDuplicateDecision is the ReviewDecision for a transaction whose
// duplicate relationship is undecided. The contract is the shared
// POSSIBLE_DUPLICATE kind, so a reopened or screenshot-created review cannot
// advertise different actions or render differently from a freshly created one.
func possibleDuplicateDecision(transactionID string, known map[string]any) ([]byte, error) {
	decision, ok := reviewdec.Preset("POSSIBLE_DUPLICATE", "transaction", transactionID)
	if !ok {
		return nil, errors.New("reviewdomain: no POSSIBLE_DUPLICATE review contract")
	}
	for key, value := range known {
		decision.KnownFacts[key] = value
	}
	return decision.JSON()
}

// ReopenTransactionReview gives a transaction that a reversal returned to
// NEEDS_REVIEW its canonical active review_item again. Resolved items are
// history and stay untouched; a new OPEN item carries the most recent complete
// ReviewDecision the household answered for this transaction. Its only caller is
// Unmerge, so a transaction whose earlier items all predate the decision
// contract reopens as POSSIBLE_DUPLICATE: reversing a merge is exactly what makes
// the duplicate relationship undecided again, whatever the original legacy
// review_type was (its allowed actions were never stored, so they cannot be
// carried over). Idempotent and race-safe: an existing active item is kept as is.
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
			AND CASE WHEN jsonb_typeof(ri.decision->'allowedActions')='array' THEN jsonb_array_length(ri.decision->'allowedActions')>0 ELSE false END
			ORDER BY ri.created_at DESC LIMIT 1
		) prev ON true
		WHERE t.id=$1 AND t.household_id=$2 AND t.status='NEEDS_REVIEW'
		ON CONFLICT (transaction_id) WHERE transaction_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN') DO NOTHING`, transactionID, household, string(fallback))
	return err
}
