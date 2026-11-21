package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrDocumentReviewInvalid = errors.New("reviewdomain: document review is no longer valid")

type DocumentReviewCommand struct {
	HouseholdID, UserID, ReviewItemID, DocumentID, ActorType string
	Action                                                   string
}

type DocumentReviewResult struct {
	SourceEventID string
}

func ResolveDocumentReview(ctx context.Context, tx pgx.Tx, cmd DocumentReviewCommand) (DocumentReviewResult, error) {
	var out DocumentReviewResult
	var status, kind string
	var decision []byte
	var document, documentSource *string
	if err := tx.QueryRow(ctx, `SELECT ri.status,ri.review_type,ri.decision,ri.document_id::text,d.source_event_id::text
		FROM review_item ri JOIN document d ON d.id=ri.document_id AND d.household_id=ri.household_id
		WHERE ri.id=$1 AND ri.household_id=$2 FOR UPDATE OF ri,d`, cmd.ReviewItemID, cmd.HouseholdID).
		Scan(&status, &kind, &decision, &document, &documentSource); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrDocumentReviewInvalid
		}
		return out, err
	}
	if (status != "OPEN" && status != "PENDING_SEND") || (kind != "DOCUMENT_CLASSIFICATION" && kind != "DOCUMENT_EXTRACTION_LOW_CONFIDENCE") || document == nil || cmd.DocumentID == "" || *document != cmd.DocumentID || documentSource == nil {
		return out, ErrDocumentReviewInvalid
	}
	var contract struct {
		AllowedActions []string `json:"allowedActions"`
	}
	if json.Unmarshal(decision, &contract) != nil || !containsAction(contract.AllowedActions, cmd.Action) || (cmd.Action != "REPROCESS_DOCUMENT" && cmd.Action != "IGNORE") {
		return out, ErrDocumentReviewInvalid
	}
	if cmd.Action == "IGNORE" {
		if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED' WHERE id=$1 AND household_id=$2`, *documentSource, cmd.HouseholdID); err != nil {
			return out, err
		}
	}
	result, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,updated_at=now()
		WHERE id=$1 AND household_id=$4 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, cmd.UserID, cmd.Action, cmd.HouseholdID)
	if err != nil {
		return out, err
	}
	if result.RowsAffected() != 1 {
		return out, ErrDocumentReviewInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, cmd.ReviewItemID); err != nil {
		return out, err
	}
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action',$5::text))`, cmd.HouseholdID, actorType, cmd.UserID, cmd.ReviewItemID, cmd.Action); err != nil {
		return out, err
	}
	if cmd.Action == "IGNORE" {
		if err := RefreshSourceLifecycle(ctx, tx, cmd.HouseholdID, *documentSource); err != nil {
			return out, err
		}
	}
	out.SourceEventID = *documentSource
	return out, nil
}
