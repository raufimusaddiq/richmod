package reviewdomain

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// FinalizeTransferReviewLifecycle refreshes the proposal/source-event state and
// completes every open transaction review for one household. Web transfer
// reconciliation and Telegram both call it (ADR-046), so the terminal review
// transition has one implementation instead of a copy per surface.
func FinalizeTransferReviewLifecycle(ctx context.Context, tx pgx.Tx, household, actorUserID, transactionID, proposedType, proposalStatus, sourceStatus string, categoryID *string, classification, action string) error {
	if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET proposed_type=$2,proposal_status=$3,category_candidate_id=$4,metadata_json=metadata_json||jsonb_build_object('transfer_classification',$5::text),updated_at=now() WHERE id IN(SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')`, transactionID, proposedType, proposalStatus, categoryID, classification); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status=$2 WHERE id IN(SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)`, transactionID, sourceStatus); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE transaction_id=$1 AND status IN('PENDING_SEND','OPEN')`, transactionID); err != nil {
		return err
	}
	if err := ResolveByTransaction(ctx, tx, Command{HouseholdID: household, ActorUserID: actorUserID, SubjectID: transactionID, Action: action}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN(SELECT id FROM review_request WHERE transaction_id=$1 AND status='RESOLVED')`, transactionID)
	return err
}
