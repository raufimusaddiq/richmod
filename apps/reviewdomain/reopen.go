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
// history and stay untouched. The new OPEN item carries a fresh ReviewDecision
// built from the kind registry and the transaction as it is now, never the old
// stored snapshot: its known facts, missing facts and allowed actions must
// describe today's subject. The kind is the most recent decision-backed kind
// the household answered for this transaction; its only caller is Unmerge, so
// a transaction with no such kind, or whose earlier question is already
// answered, reopens as POSSIBLE_DUPLICATE: reversing a merge is exactly what
// makes the duplicate relationship undecided again.
//
// A newly reopened item is projected to Telegram through the shared projection
// path by a PROJECT_TELEGRAM_REVIEW job, so the household is asked again where
// the review started. Idempotent and race-safe: an existing active item is kept
// as is and is not projected twice.
func ReopenTransactionReview(ctx context.Context, tx pgx.Tx, household, transactionID string) error {
	var amount, at, kind, previousType, previousItem, categorySlug string
	if err := tx.QueryRow(ctx, `SELECT t.amount::text,t.transaction_at::text,t.type,COALESCE(prev.review_type,''),COALESCE(prev.id::text,''),COALESCE(c.slug,'')
		FROM transaction t
		LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id
		LEFT JOIN LATERAL (
			SELECT ri.id,ri.review_type FROM review_item ri
			WHERE ri.transaction_id=t.id AND COALESCE(ri.decision->>'reasonCode','')<>''
			AND CASE WHEN jsonb_typeof(ri.decision->'allowedActions')='array' THEN jsonb_array_length(ri.decision->'allowedActions')>0 ELSE false END
			ORDER BY ri.created_at DESC LIMIT 1
		) prev ON true
		WHERE t.id=$1 AND t.household_id=$2`, transactionID, household).Scan(&amount, &at, &kind, &previousType, &previousItem, &categorySlug); err != nil {
		return err
	}
	decision := reopenedDecision(transactionID, previousType, kind, categorySlug)
	decision.KnownFacts["amount_idr"] = amount
	decision.KnownFacts["transaction_at"] = at
	decision.KnownFacts["type"] = kind
	if categorySlug != "" {
		decision.KnownFacts["category"] = categorySlug
	}
	if decision.Provenance == nil {
		decision.Provenance = map[string]any{}
	}
	decision.Provenance["reopened"] = "MERGE_REVERSED"
	if previousItem != "" {
		decision.Provenance["previous_review_item_id"] = previousItem
	}
	encoded, err := decision.JSON()
	if err != nil {
		return err
	}
	var itemID string
	err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision,preferred_user_id)
		SELECT t.household_id,t.id,$3,'OPEN',$4::jsonb,t.created_by_user_id
		FROM transaction t
		WHERE t.id=$1 AND t.household_id=$2 AND t.status='NEEDS_REVIEW'
		ON CONFLICT (transaction_id) WHERE transaction_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN') DO NOTHING
		RETURNING id`, transactionID, household, decision.ReasonCode, string(encoded)).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return EnqueueReviewProjection(ctx, tx, household, itemID)
}

// reopenedDecision picks the fresh contract for a reopened transaction review.
// The previous kind is reused only while its question is still open on the
// subject as it is now: a category question whose category is already set is
// answered, so the reopened review asks the duplicate question instead.
func reopenedDecision(transactionID, previousType, transactionType, categorySlug string) reviewdec.Decision {
	if decision, ok := reviewdec.Preset(previousType, "transaction", transactionID); ok && previousType != "POSSIBLE_DUPLICATE" {
		missing := make([]string, 0, len(decision.MissingFacts))
		for _, fact := range decision.MissingFacts {
			if fact == "category" && (categorySlug != "" || transactionType != "EXPENSE") {
				continue
			}
			missing = append(missing, fact)
		}
		if len(missing) > 0 {
			decision.MissingFacts = missing
			return decision
		}
	}
	decision, _ := reviewdec.Preset("POSSIBLE_DUPLICATE", "transaction", transactionID)
	return decision
}

// EnqueueReviewProjection asks the worker to project one review item to
// Telegram through the shared projection path. Surfaces that cannot render a
// card themselves (the API) use it; the projection is idempotent per item.
func EnqueueReviewProjection(ctx context.Context, tx pgx.Tx, household, itemID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('PROJECT_TELEGRAM_REVIEW',jsonb_build_object('household_id',$1::uuid,'review_item_id',$2::uuid))`, household, itemID)
	return err
}
