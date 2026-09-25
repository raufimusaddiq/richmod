package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// DuplicateCommand carries one canonical duplicate-merge decision: the open
// review transaction is merged into an existing confirmed transaction.
type DuplicateCommand struct {
	HouseholdID   string
	ActorUserID   string
	TransactionID string
	// TargetTransactionID is the confirmed household transaction keeping the
	// canonical record.
	TargetTransactionID string
	// ReviewItemID pins one exact item (Telegram binding). Empty completes every
	// open item for the transaction, which is the Web behavior.
	ReviewItemID string
	RequestID    string
	Action       string
}

// DuplicateResult reports the created merge so each surface can answer with its
// own shape.
type DuplicateResult struct{ MergeID string }

var (
	// ErrDuplicateTargetInvalid reports a merge target that is not an open-review
	// source plus a confirmed household target in the same shape and window.
	ErrDuplicateTargetInvalid = errors.New("reviewdomain: invalid duplicate merge target")
	// ErrAlreadyMerged reports a source transaction that already has an active merge.
	ErrAlreadyMerged = errors.New("reviewdomain: review is already merged")
)

// mergeWindowHours is the deterministic window two transactions must share to be
// the same real event (ADR-039 duplicate safety).
const mergeWindowHours = 72

// MergeDuplicateReview is the canonical duplicate-resolution operation. It locks
// both transactions, revalidates that the target is still a deterministic
// duplicate candidate, copies evidence onto the canonical transaction, voids the
// duplicate, and completes the review. Web and Telegram both call it (ADR-046).
func MergeDuplicateReview(ctx context.Context, tx pgx.Tx, cmd DuplicateCommand) (DuplicateResult, error) {
	var result DuplicateResult
	if cmd.TargetTransactionID == "" || cmd.TargetTransactionID == cmd.TransactionID {
		return result, ErrDuplicateTargetInvalid
	}
	var sourceType, sourceAmount, sourceCurrency string
	var sourceTime time.Time
	err := tx.QueryRow(ctx, "SELECT type,amount::text,currency,transaction_at FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE", cmd.TransactionID, cmd.HouseholdID).Scan(&sourceType, &sourceAmount, &sourceCurrency, &sourceTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrDuplicateTargetInvalid
	}
	if err != nil {
		return result, err
	}
	var targetType, targetAmount, targetCurrency string
	var targetTime time.Time
	err = tx.QueryRow(ctx, "SELECT type,amount::text,currency,transaction_at FROM transaction WHERE id=$1 AND household_id=$2 AND status='CONFIRMED' FOR UPDATE", cmd.TargetTransactionID, cmd.HouseholdID).Scan(&targetType, &targetAmount, &targetCurrency, &targetTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrDuplicateTargetInvalid
	}
	if err != nil {
		return result, err
	}
	if sourceType != targetType || sourceAmount != targetAmount || sourceCurrency != targetCurrency || math.Abs(targetTime.Sub(sourceTime).Hours()) > mergeWindowHours {
		return result, ErrDuplicateTargetInvalid
	}
	if err = tx.QueryRow(ctx, "INSERT INTO reconciliation_merge(household_id,source_transaction_id,target_transaction_id,status,created_by_user_id) VALUES($1,$2,$3,'ACTIVE',$4) RETURNING id", cmd.HouseholdID, cmd.TransactionID, cmd.TargetTransactionID, cmd.ActorUserID).Scan(&result.MergeID); err != nil {
		return result, ErrAlreadyMerged
	}
	type evidenceCopy struct {
		originalID, sourceEventID, evidenceType string
		confidence                              *string
		metadata                                json.RawMessage
	}
	rows, err := tx.Query(ctx, "SELECT id,source_event_id,evidence_type,confidence,metadata_json FROM transaction_evidence WHERE transaction_id=$1", cmd.TransactionID)
	if err != nil {
		return result, err
	}
	var evidence []evidenceCopy
	for rows.Next() {
		var value evidenceCopy
		if err := rows.Scan(&value.originalID, &value.sourceEventID, &value.evidenceType, &value.confidence, &value.metadata); err != nil {
			rows.Close()
			return result, err
		}
		evidence = append(evidence, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, value := range evidence {
		var copiedID string
		err := tx.QueryRow(ctx, "INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,$3,$4,$5) ON CONFLICT(transaction_id,source_event_id) DO NOTHING RETURNING id", cmd.TargetTransactionID, value.sourceEventID, value.evidenceType, value.confidence, value.metadata).Scan(&copiedID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO reconciliation_merge_evidence(merge_id,original_evidence_id,copied_evidence_id) VALUES($1,$2,$3)", result.MergeID, value.originalID, copiedID); err != nil {
			return result, err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE transaction SET status='VOIDED',confirmed_at=NULL,voided_at=now(),updated_at=now() WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW'", cmd.TransactionID, cmd.HouseholdID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET proposal_status='MERGED',metadata_json=metadata_json||jsonb_build_object('merged_into',$2::uuid),updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')", cmd.TransactionID, cmd.TargetTransactionID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "UPDATE source_event s SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END WHERE s.id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)", cmd.TransactionID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "UPDATE review_request SET status='CANCELLED' WHERE transaction_id=$1 AND status IN ('PENDING_SEND','OPEN')", cmd.TransactionID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE transaction_id=$1 AND status='CANCELLED')", cmd.TransactionID); err != nil {
		return result, err
	}
	action := cmd.Action
	if action == "" {
		action = "MERGE_REVIEW"
	}
	if cmd.ReviewItemID == "" {
		return result, ResolveByTransaction(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, SubjectID: cmd.TransactionID, Action: action})
	}
	return result, ResolveByID(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, ReviewItemID: cmd.ReviewItemID, RequestID: cmd.RequestID, SubjectID: cmd.TransactionID, Action: action})
}
