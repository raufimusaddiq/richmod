package financialemail

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

// TerminalFailureTx runs inside the transaction that marks a
// PROCESS_FINANCIAL_EMAIL job FAILED because its attempts ran out. The source
// event is finalized as FAILED and the household gets a dismissable Tindakan
// item; a source that already reached an outcome is left alone.
func (p *Processor) TerminalFailureTx(ctx context.Context, tx pgx.Tx, sourceEventID string) error {
	if !reviewdomain.IsSourceEventID(sourceEventID) {
		return nil
	}
	var householdID string
	err := tx.QueryRow(ctx, `UPDATE source_event SET processing_status='FAILED' WHERE id=$1 AND source_type='FINANCIAL_EMAIL' AND processing_status IN ('RECEIVED','PROCESSING','FAILED') RETURNING household_id::text`, sourceEventID).Scan(&householdID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return reviewdomain.RecordFailedSourceAction(ctx, tx, reviewdomain.FailedSource{
		HouseholdID: householdID, SourceEventID: sourceEventID, SourceType: "FINANCIAL_EMAIL", Reason: "ERROR",
	})
}

// TerminalPreviewFailureTx runs inside the transaction that marks a
// PROCESS_FINANCIAL_EMAIL_PREVIEW job FAILED. A preview the job never finished
// would stay PROCESSING forever and block activating the source.
func (p *Processor) TerminalPreviewFailureTx(ctx context.Context, tx pgx.Tx, previewID string, cause error) error {
	if !reviewdomain.IsSourceEventID(previewID) {
		return nil
	}
	message := "preview failed"
	if cause != nil {
		message = cause.Error()
	}
	_, err := tx.Exec(ctx, `UPDATE financial_email_preview SET status='FAILED',error_message=$2,updated_at=now() WHERE id=$1 AND status IN ('PENDING','PROCESSING')`, previewID, message)
	return err
}
