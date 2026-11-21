package reviewdomain

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type RejectCommand struct {
	HouseholdID   string
	ActorUserID   string
	TransactionID string
	ReviewItemID  string
	RequestID     string
	Action        string
}

func RejectTransactionReview(ctx context.Context, tx pgx.Tx, cmd RejectCommand) error {
	if err := ValidateTransactionReview(ctx, tx, cmd.HouseholdID, cmd.TransactionID); err != nil {
		return err
	}
	if protected, err := ActiveMergeTarget(ctx, tx, cmd.HouseholdID, cmd.TransactionID); err != nil {
		return err
	} else if protected {
		return ErrMergeTargetActive
	}
	result, err := tx.Exec(ctx, "UPDATE transaction SET status='VOIDED',confirmed_at=NULL,voided_at=now(),updated_at=now() WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW'", cmd.TransactionID, cmd.HouseholdID)
	if err != nil {
		return MergeTargetError(err)
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')", cmd.TransactionID); err != nil {
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
