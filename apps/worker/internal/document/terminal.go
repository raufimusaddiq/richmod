package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

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

func unpaidInvoiceOnly(value screenshotExtraction, documentType string) bool {
	if documentType != "BILL_OR_INVOICE" || value.PaymentStatus == "PAID" {
		return false
	}
	for _, issue := range screenshotValidationIssues(value, documentType) {
		switch {
		case issue.Field == "payment_status" && issue.Code == "PAYMENT_NOT_CONFIRMED":
		case issue.Field == "transactions" && issue.Code == "INVALID_ROW_COUNT" && len(value.Transactions) == 0:
		default:
			return false
		}
	}
	return true
}

func (p *Processor) failScreenshotExtraction(ctx context.Context, documentID, householdID, sourceID, documentType string, value screenshotExtraction, model string, cause error) error {
	if unpaidInvoiceOnly(value, documentType) {
		return p.persistInvalidDocumentExtraction(ctx, documentID, sourceID, "TRANSACTION_SCREENSHOT", value, value.Confidence, model, cause)
	}
	return p.persistScreenshotExtractionFailure(ctx, documentID, householdID, sourceID, value, model, cause)
}

func (p *Processor) persistScreenshotExtractionFailure(ctx context.Context, documentID, householdID, sourceID string, value screenshotExtraction, model string, cause error) error {
	output, _ := json.Marshal(value)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var storedConfidence any
	if !math.IsNaN(value.Confidence) && value.Confidence >= 0 && value.Confidence <= 1 {
		storedConfidence = value.Confidence
	}
	if _, err := tx.Exec(ctx, `INSERT INTO document_extraction(document_id,stage,schema_version,output_json,confidence,gateway_model,validated) VALUES($1,'TRANSACTION_SCREENSHOT','1',$2::jsonb,$3,$4,false) ON CONFLICT (document_id,stage,schema_version) DO UPDATE SET output_json=EXCLUDED.output_json,confidence=EXCLUDED.confidence,gateway_model=EXCLUDED.gateway_model WHERE NOT document_extraction.validated`, documentID, string(output), storedConfidence, model); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE document SET status='FAILED',updated_at=now() WHERE id=$1 AND household_id=$2`, documentID, householdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','REJECT_DOCUMENT_EXTRACTION','source_event',$2,jsonb_build_object('document_id',$3::uuid,'stage','TRANSACTION_SCREENSHOT','reason',$4::text))`, householdID, sourceID, documentID, truncate(cause)); err != nil {
		return err
	}
	decision, ok := reviewdec.Preset("DOCUMENT_EXTRACTION_LOW_CONFIDENCE", "document", documentID)
	if !ok {
		return fmt.Errorf("no review decision preset for DOCUMENT_EXTRACTION_LOW_CONFIDENCE")
	}
	decision.SourceEventID = sourceID
	decision.Provenance["validationFailure"] = truncate(cause)
	encoded, err := decision.JSON()
	if err != nil {
		return err
	}
	var reviewItemID string
	if err := tx.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,document_id,review_type,status,decision) VALUES($1,$2,$3,'DOCUMENT_EXTRACTION_LOW_CONFIDENCE','OPEN',$4::jsonb) ON CONFLICT DO NOTHING RETURNING id`, householdID, sourceID, documentID, string(encoded)).Scan(&reviewItemID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM review_item WHERE document_id=$1 AND household_id=$2 AND status IN ('PENDING_SEND','OPEN') LIMIT 1`, documentID, householdID).Scan(&reviewItemID); err != nil {
			return err
		}
	}
	if err := p.projectDocumentReview(ctx, tx, householdID, sourceID, reviewItemID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($1,'WORKER','CREATE_DOCUMENT_REVIEW','document',$2,jsonb_build_object('review_type','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','review_item_id',$3::uuid,'source_event_id',$4::uuid))`, householdID, documentID, reviewItemID, sourceID); err != nil {
		return err
	}
	if err := reviewdomain.RefreshSourceLifecycle(ctx, tx, householdID, sourceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
