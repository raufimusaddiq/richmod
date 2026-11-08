package document

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// TerminalDownstreamFailureTx runs inside the transaction that marks a
// PROCESS_PAYSLIP / PROCESS_RECEIPT / PROCESS_TRANSACTION_SCREENSHOT job FAILED
// because its attempts ran out. Classification already settled the source as
// PROCESSED and left the document CLASSIFIED while the job extracted the
// details, so without this step a job that never succeeds leaves a document that
// looks done and a household that is never told.
//
// A document that is still CLASSIFIED never produced an outcome; it becomes
// FAILED. The source follows only when nothing else on it produced evidence, a
// proposal, a review or a finished sibling document, and then the household
// gets the same dismissable Tindakan item every other failed source gets.
func (p *Processor) TerminalDownstreamFailureTx(ctx context.Context, tx pgx.Tx, documentID string) error {
	if !reviewdomain.IsSourceEventID(documentID) {
		return nil
	}
	var householdID, sourceID, sourceType string
	err := tx.QueryRow(ctx, `UPDATE document SET status='FAILED',updated_at=now() WHERE id=$1 AND status='CLASSIFIED' RETURNING household_id::text,source_event_id::text`, documentID).Scan(&householdID, &sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND household_id=$2
		AND processing_status IN ('RECEIVED','PROCESSING','PROCESSED')
		AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id=$1)
		AND NOT EXISTS (SELECT 1 FROM transaction_proposal tp WHERE tp.source_event_id=$1)
		AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id=$1 OR ri.document_id=$3)
		AND NOT EXISTS (SELECT 1 FROM document d WHERE d.source_event_id=$1 AND d.id<>$3 AND d.status IN ('EXTRACTED','NEEDS_REVIEW'))
		RETURNING source_type`, sourceID, householdID, documentID).Scan(&sourceType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','DOCUMENT_EXTRACTION_FAILED','source_event',$2,jsonb_build_object('document_id',$3::uuid))`, householdID, sourceID, documentID); err != nil {
		return err
	}
	return reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{
		HouseholdID: householdID, SourceEventID: sourceID, SourceType: sourceType, Reason: "ERROR",
	})
}
