package reviewdomain

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// RejectCommand carries one canonical transaction-review rejection.
type RejectCommand struct {
	HouseholdID   string
	ActorUserID   string
	TransactionID string
	// ReviewItemID pins one exact item (Telegram binding). Empty completes every
	// open item for the transaction, which is the Web behavior.
	ReviewItemID string
	// RequestID is the exact Telegram projection, used only with ReviewItemID.
	RequestID string
	Action    string
}

// RejectTransactionReview performs the canonical transaction-review rejection: it
// locks the subject, voids the transaction, rejects its proposal, refreshes the
// source-event states, cancels the Telegram projection(s), and completes the
// review item(s). Web and Telegram both call this (ADR-046).
func RejectTransactionReview(ctx context.Context, tx pgx.Tx, cmd RejectCommand) error {
	if err := ValidateTransactionReview(ctx, tx, cmd.HouseholdID, cmd.TransactionID); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, "UPDATE transaction SET status='VOIDED',confirmed_at=NULL,voided_at=now(),updated_at=now() WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW'", cmd.TransactionID, cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')", cmd.TransactionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE source_event s SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='CONFIRMED') THEN 'PROCESSED' ELSE 'IGNORED' END WHERE s.id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)", cmd.TransactionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE review_request SET status='CANCELLED' WHERE transaction_id=$1 AND status IN ('PENDING_SEND','OPEN')", cmd.TransactionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE transaction_id=$1 AND status='CANCELLED')", cmd.TransactionID); err != nil {
		return err
	}
	action := cmd.Action
	if action == "" {
		action = "REJECT_REVIEW"
	}
	if cmd.ReviewItemID == "" {
		return ResolveByTransaction(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, SubjectID: cmd.TransactionID, Action: action})
	}
	return ResolveByID(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, ReviewItemID: cmd.ReviewItemID, RequestID: cmd.RequestID, SubjectID: cmd.TransactionID, Action: action})
}
