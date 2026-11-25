package telegram

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

func (p *Processor) transactionConfirmableWithoutCategory(ctx context.Context, tx pgx.Tx, transactionID string) bool {
	var kind string
	var categoryID *string
	if err := tx.QueryRow(ctx, `SELECT type,category_id::text FROM transaction WHERE id=$1 FOR UPDATE`, transactionID).Scan(&kind, &categoryID); err != nil {
		return false
	}
	return kind != "EXPENSE" || categoryID != nil
}

func reviewNeedsCategory(ctx context.Context, tx pgx.Tx, reviewID string) bool {
	var decision []byte
	if err := tx.QueryRow(ctx, `SELECT ri.decision FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id WHERE r.id=$1`, reviewID).Scan(&decision); err != nil {
		return true
	}
	var stored struct {
		MissingFacts []string `json:"missingFacts"`
	}
	if json.Unmarshal(decision, &stored) != nil {
		return true
	}
	return contains(stored.MissingFacts, "category")
}

type duplicateCandidate struct {
	ID, Amount, Merchant string
	At                   time.Time
}

func duplicateCandidateRows(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, householdID, transactionID, sourceType, sourceCurrency, sourceAmount string, sourceAt time.Time) ([]duplicateCandidate, error) {
	rows, err := q.Query(ctx, `WITH prior AS (SELECT jsonb_array_elements_text(ri.decision->'knownFacts'->'prior_match_transaction_ids') AS id FROM review_item ri WHERE ri.household_id=$1 AND ri.transaction_id=$2 AND jsonb_typeof(ri.decision->'knownFacts'->'prior_match_transaction_ids')='array')
		SELECT t.id::text,t.amount::text,COALESCE(t.counterparty_name,t.description,'Transaksi'),t.transaction_at FROM transaction t WHERE t.household_id=$1 AND t.id<>$2 AND (t.status='CONFIRMED' OR (t.status='NEEDS_REVIEW' AND t.id::text IN (SELECT id FROM prior))) AND ((t.type=$3 AND t.currency=$4 AND t.amount=$5::numeric AND t.transaction_at BETWEEN $6::timestamptz-interval '72 hours' AND $6::timestamptz+interval '72 hours') OR (t.type=$3 AND t.currency=$4 AND t.id::text IN (SELECT id FROM prior))) ORDER BY (t.id::text IN (SELECT id FROM prior)) DESC,abs(extract(epoch FROM (t.transaction_at-$6::timestamptz)))`, householdID, transactionID, sourceType, sourceCurrency, sourceAmount, sourceAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []duplicateCandidate
	for rows.Next() {
		var candidate duplicateCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Amount, &candidate.Merchant, &candidate.At); err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	return out, rows.Err()
}
