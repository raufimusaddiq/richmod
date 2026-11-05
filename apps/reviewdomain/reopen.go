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
// history and stay untouched. Its only caller is Unmerge, and reversing a merge
// is exactly what makes the duplicate relationship undecided again, so the new
// OPEN item is a fresh POSSIBLE_DUPLICATE decision from the kind registry. It
// never copies an old snapshot: every other residual is recomputed from the
// transaction and its evidence as they are now (an expense with no category, a
// date that only fell back to the received time, an unclassified transfer), so
// a fact the household already supplied is not asked again. The previous kind
// is kept as provenance only.
//
// A newly reopened item is projected to Telegram through the shared projection
// path by a PROJECT_TELEGRAM_REVIEW job, so the household is asked again where
// the review started. Idempotent and race-safe: an existing active item is kept
// as is and is not projected twice.
func ReopenTransactionReview(ctx context.Context, tx pgx.Tx, household, transactionID string) error {
	var amount, at, kind, previousType, previousItem, categorySlug string
	var dateFallback bool
	if err := tx.QueryRow(ctx, `SELECT t.amount::text,t.transaction_at::text,t.type,COALESCE(prev.review_type,''),COALESCE(prev.id::text,''),COALESCE(c.slug,''),
		EXISTS(SELECT 1 FROM transaction_proposal p JOIN transaction_evidence te ON p.id=NULLIF(te.metadata_json->>'proposal_id','')::uuid WHERE te.transaction_id=t.id AND p.household_id=t.household_id AND p.metadata_json->>'date_known'='false' AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE')
		FROM transaction t
		LEFT JOIN category c ON c.id=t.category_id AND c.household_id=t.household_id AND c.active
		LEFT JOIN LATERAL (
			SELECT ri.id,ri.review_type FROM review_item ri
			WHERE ri.transaction_id=t.id ORDER BY ri.created_at DESC LIMIT 1
		) prev ON true
		WHERE t.id=$1 AND t.household_id=$2`, transactionID, household).Scan(&amount, &at, &kind, &previousType, &previousItem, &categorySlug, &dateFallback); err != nil {
		return err
	}
	decision, ok := reviewdec.Preset("POSSIBLE_DUPLICATE", "transaction", transactionID)
	if !ok {
		return errors.New("reviewdomain: no POSSIBLE_DUPLICATE review contract")
	}
	decision.MissingFacts = append(decision.MissingFacts, reopenedResiduals(kind, categorySlug != "", dateFallback)...)
	decision.KnownFacts["amount_idr"] = amount
	decision.KnownFacts["type"] = kind
	if !dateFallback {
		decision.KnownFacts["transaction_at"] = at
	}
	if categorySlug != "" {
		decision.KnownFacts["category"] = categorySlug
	}
	decision.Provenance["reopened"] = "MERGE_REVERSED"
	if previousItem != "" {
		decision.Provenance["previous_review_item_id"] = previousItem
		decision.Provenance["previous_review_type"] = previousType
	}
	encoded, err := decision.JSON()
	if err != nil {
		return err
	}
	var itemID string
	err = tx.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision,preferred_user_id)
		SELECT t.household_id,t.id,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb,t.created_by_user_id
		FROM transaction t
		WHERE t.id=$1 AND t.household_id=$2 AND t.status='NEEDS_REVIEW'
		ON CONFLICT (transaction_id) WHERE transaction_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN') DO NOTHING
		RETURNING id`, transactionID, household, string(encoded)).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return EnqueueReviewProjection(ctx, tx, household, itemID)
}

// reopenedResiduals lists the facts besides the duplicate relationship that the
// transaction still lacks, read from canonical state only: an expense without a
// category, a date that is only the received-time fallback, and an incoming or
// outgoing row whose transfer relationship was never classified.
func reopenedResiduals(transactionType string, hasCategory, dateFallback bool) []string {
	var missing []string
	if transactionType == "EXPENSE" && !hasCategory {
		missing = append(missing, "category")
	}
	if dateFallback {
		missing = append(missing, "transaction_at")
	}
	if transactionType == "UNCLASSIFIED" {
		missing = append(missing, "transfer_relationship")
	}
	return missing
}

// EnqueueReviewProjection asks the worker to project one review item to
// Telegram through the shared projection path. Surfaces that cannot render a
// card themselves (the API) use it; the projection is idempotent per item.
func EnqueueReviewProjection(ctx context.Context, tx pgx.Tx, household, itemID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO job(type,payload_json) VALUES('PROJECT_TELEGRAM_REVIEW',jsonb_build_object('household_id',$1::uuid,'review_item_id',$2::uuid))`, household, itemID)
	return err
}
