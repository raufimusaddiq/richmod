package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// ConfirmedReviewReopenable is the SQL predicate (alias ri = the resolved review_item, t = its
// transaction) shared by Resolved and ReopenReview for undoing a plain category/date confirmation.
// It is deliberately narrow (ADR-054): the item is the transaction's latest, it was resolved by
// CONFIRM_REVIEW, and the transaction is an ordinary confirmed expense/income/refund that no merge
// or salary event depends on. Anything else is voided, unmerged or corrected through its own path.
const ConfirmedReviewReopenable = `(ri.status='RESOLVED' AND ri.resolution_action='CONFIRM_REVIEW' AND t.status='CONFIRMED'
	AND t.type IN ('EXPENSE','INCOME','REFUND')
	AND NOT EXISTS (SELECT 1 FROM review_item n WHERE n.transaction_id=ri.transaction_id AND (n.created_at>ri.created_at OR (n.created_at=ri.created_at AND n.id>ri.id)))
	AND NOT EXISTS (SELECT 1 FROM reconciliation_merge m WHERE m.household_id=t.household_id AND m.status='ACTIVE' AND t.id IN (m.source_transaction_id,m.target_transaction_id))
	AND NOT EXISTS (SELECT 1 FROM salary_event se WHERE se.transaction_id=t.id))`

// reopenTransactionResolution reopens a resolved transaction review. A transaction already back in
// NEEDS_REVIEW (reversed merge) takes the existing path; a bounded CONFIRMED one is moved back to
// NEEDS_REVIEW with a fresh OPEN item (the resolved item stays as history).
func reopenTransactionResolution(ctx context.Context, tx pgx.Tx, cmd ReopenCommand, transactionID, kind string, decision []byte) (string, error) {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1 AND household_id=$2 FOR UPDATE`, transactionID, cmd.HouseholdID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrReopenNotReversible
		}
		return "", err
	}
	if status == "NEEDS_REVIEW" {
		id, err := reopenTransaction(ctx, tx, cmd.HouseholdID, transactionID, cmd.ActorUserID)
		if err == nil && id == "" {
			return "", ErrReopenNotReversible
		}
		return id, err
	}
	var reopenable bool
	var transactionAt time.Time
	err := tx.QueryRow(ctx, `SELECT `+ConfirmedReviewReopenable+`,t.transaction_at
		FROM review_item ri JOIN transaction t ON t.id=ri.transaction_id AND t.household_id=ri.household_id
		WHERE ri.id=$1 AND ri.household_id=$2`, cmd.ReviewItemID, cmd.HouseholdID).Scan(&reopenable, &transactionAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !reopenable) {
		return "", ErrReopenNotReversible
	}
	if err != nil {
		return "", err
	}
	var stored struct {
		MissingFacts []string `json:"missingFacts"`
	}
	_ = json.Unmarshal(decision, &stored)
	// Confirmation rewrites missingFacts to what remained, so the review kind is the durable signal
	// that a category was the question.
	clearCategory := kind == "AMBIGUOUS_CATEGORY" || kind == "UNKNOWN_MERCHANT" || kind == "TRANSACTION_FACTS_MISSING"
	for _, fact := range stored.MissingFacts {
		clearCategory = clearCategory || fact == "category"
	}
	// The review asked for the category, so the answer leaves with the confirmation and the new
	// item asks again cleanly; any other field keeps the household's value.
	moved, err := tx.Exec(ctx, `UPDATE transaction SET status='NEEDS_REVIEW',confirmed_at=NULL,category_id=CASE WHEN $3 THEN NULL ELSE category_id END,updated_at=now() WHERE id=$1 AND household_id=$2 AND status='CONFIRMED'`, transactionID, cmd.HouseholdID, clearCategory)
	if err != nil {
		return "", err
	}
	if moved.RowsAffected() != 1 {
		return "", ErrReopenNotReversible
	}
	if clearCategory {
		// Confirm emptied missingFacts and recorded the chosen category as known; the reopened item
		// must ask for the category again, with no stale answer.
		var rebuilt reviewdec.Decision
		if json.Unmarshal(decision, &rebuilt) != nil || rebuilt.ReasonCode == "" {
			preset, ok := reviewdec.Preset(kind, "transaction", transactionID)
			if !ok {
				return "", ErrReopenNotReversible
			}
			rebuilt = preset
		}
		delete(rebuilt.KnownFacts, "category")
		if !slices.Contains(rebuilt.MissingFacts, "category") {
			rebuilt.MissingFacts = append(rebuilt.MissingFacts, "category")
		}
		if decision, err = rebuilt.JSON(); err != nil {
			return "", err
		}
	}
	id, err := insertReopened(ctx, tx, cmd, kind, "transaction_id", transactionID, "transaction", decision)
	if err != nil {
		return "", err
	}
	// The transaction left the totals: an open cycle review built on them is recomputed.
	if err := RefreshOpenCycleResidualsFor(ctx, tx, cmd.HouseholdID, transactionAt, cmd.ActorUserID, CycleReopenTransactionVoided); err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT te.source_event_id::text FROM transaction_evidence te WHERE te.transaction_id=$1 AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE'`, transactionID)
	if err != nil {
		return "", err
	}
	sources, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	for _, source := range sources {
		if err := RefreshSourceLifecycle(ctx, tx, cmd.HouseholdID, source); err != nil {
			return "", err
		}
	}
	return id, nil
}
