package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrMissingAmountReviewInvalid = errors.New("reviewdomain: missing-amount review invalid")

type MissingAmountCommand struct {
	HouseholdID, UserID, ReviewItemID, ProposalID, SourceEventID, ActorType string
	AmountIDR                                                               *string
	CategoryID, TransactionDate                                             string
	IncomeConfirmed                                                         bool
}

type MissingAmountResult struct{ TransactionID string }

// ResolveMissingAmountProposal accepts one household-provided amount. It never
// materializes a canonical row until every required fact is present. Any
// plausible same-amount transaction remains in duplicate review.
func ResolveMissingAmountProposal(ctx context.Context, tx pgx.Tx, cmd MissingAmountCommand) (MissingAmountResult, error) {
	var result MissingAmountResult
	if cmd.AmountIDR == nil || !ValidBankAmountIDR(*cmd.AmountIDR) {
		return result, ErrMissingAmountReviewInvalid
	}
	var typ, merchant, description, source, document string
	var category *string
	var at time.Time
	var dateKnown bool
	var rowIndex int
	var confidence float64
	err := tx.QueryRow(ctx, `SELECT p.proposed_type,COALESCE(p.merchant_raw,''),COALESCE(p.description,''),p.source_event_id::text,p.category_candidate_id,p.transaction_at,(p.metadata_json->>'date_known')::boolean,(p.metadata_json->>'row_index')::integer,COALESCE(p.metadata_json->>'document_id',''),p.confidence
		FROM review_item ri JOIN transaction_proposal p ON p.id=ri.proposal_id
		JOIN source_event s ON s.id=p.source_event_id AND s.household_id=p.household_id
		JOIN document d ON d.id=NULLIF(p.metadata_json->>'document_id','')::uuid AND d.household_id=p.household_id AND d.source_event_id=s.id
		WHERE ri.id=$1 AND ri.household_id=$2 AND ri.review_type='MISSING_AMOUNT' AND ri.status IN ('OPEN','PENDING_SEND')
		AND d.document_type IN ('BANK_TRANSACTION_SCREENSHOT','EWALLET_SCREENSHOT','TRANSACTION_HISTORY_SCREENSHOT','TRANSFER_PROOF','BILL_OR_INVOICE')
		AND p.id=$3 AND p.household_id=$2 AND p.amount IS NULL AND p.proposal_status='NEEDS_REVIEW'
		FOR UPDATE OF ri,p`, cmd.ReviewItemID, cmd.HouseholdID, cmd.ProposalID).Scan(&typ, &merchant, &description, &source, &category, &at, &dateKnown, &rowIndex, &document, &confidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrMissingAmountReviewInvalid
	}
	if err != nil {
		return result, err
	}
	if source != cmd.SourceEventID {
		return result, ErrMissingAmountReviewInvalid
	}
	// Retain the extracted/fallback instant as a second duplicate probe anchor.
	// Replacing it with a day-only user date can miss the same event at the edge
	// of the original 72-hour matching window.
	probeAt := at
	if !dateKnown {
		if cmd.TransactionDate == "" {
			return result, ErrMissingAmountReviewInvalid
		}
		jakarta, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			return result, err
		}
		parsed, err := time.ParseInLocation("2006-01-02", cmd.TransactionDate, jakarta)
		if err != nil || parsed.Format("2006-01-02") != cmd.TransactionDate {
			return result, ErrMissingAmountReviewInvalid
		}
		at = parsed
	}
	if typ == "EXPENSE" {
		if cmd.CategoryID != "" {
			if err := ValidateCategoryForHousehold(ctx, tx, cmd.HouseholdID, cmd.CategoryID); err != nil {
				return result, ErrMissingAmountReviewInvalid
			}
			category = &cmd.CategoryID
		}
		if category == nil {
			return result, ErrMissingAmountReviewInvalid
		}
		if cmd.CategoryID == "" && ValidateCategoryForHousehold(ctx, tx, cmd.HouseholdID, *category) != nil {
			return result, ErrMissingAmountReviewInvalid
		}
	} else if typ != "INCOME" || !cmd.IncomeConfirmed {
		return result, ErrMissingAmountReviewInvalid
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND type=$2 AND currency='IDR' AND amount=$3::numeric AND (transaction_at BETWEEN $4::timestamptz-interval '72 hours' AND $4::timestamptz+interval '72 hours' OR transaction_at BETWEEN $5::timestamptz-interval '72 hours' AND $5::timestamptz+interval '96 hours'))`, cmd.HouseholdID, typ, *cmd.AmountIDR, probeAt, at).Scan(&duplicate); err != nil {
		return result, err
	}
	status, proposalStatus := "CONFIRMED", "ACCEPTED"
	if duplicate {
		status, proposalStatus = "NEEDS_REVIEW", "NEEDS_REVIEW"
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET amount=$2::numeric,transaction_at=$3,category_candidate_id=$4,proposal_status=$5,metadata_json=metadata_json||jsonb_build_object('amount_source','USER','date_known',true),updated_at=now() WHERE id=$1`, cmd.ProposalID, *cmd.AmountIDR, at, category, proposalStatus); err != nil {
		return result, err
	}
	var merchantID *string
	if merchant != "" {
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,regexp_replace(trim($2), '[[:space:]]+', ' ', 'g')) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=now() RETURNING id`, cmd.HouseholdID, merchant).Scan(&id); err != nil {
			return result, err
		}
		merchantID = &id
	}
	if err := tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,category_id,description,source_confidence,classification_confidence,confirmed_at) VALUES($1,$2,$3,$4::numeric,'IDR',$5,$6,$7,NULLIF($8,''),$9,$9,CASE WHEN $3='CONFIRMED' THEN now() ELSE NULL END) RETURNING id`, cmd.HouseholdID, typ, status, *cmd.AmountIDR, at, merchantID, category, description, confidence).Scan(&result.TransactionID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TRANSACTION_SCREENSHOT',$3,jsonb_build_object('proposal_id',$4::uuid,'document_id',$5::uuid,'row_index',$6::integer,'amount_source','USER'))`, result.TransactionID, source, confidence, cmd.ProposalID, document, rowIndex); err != nil {
		return result, err
	}
	values, _ := json.Marshal(map[string]string{"amountIdr": *cmd.AmountIDR, "transactionId": result.TransactionID})
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='SET_AMOUNT',resolution_values=$3::jsonb,updated_at=now() WHERE id=$1`, cmd.ReviewItemID, cmd.UserID, string(values)); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, cmd.ReviewItemID); err != nil {
		return result, err
	}
	if duplicate {
		decision := map[string]any{"version": 1, "subject": map[string]any{"type": "transaction", "id": result.TransactionID}, "reasonCode": "POSSIBLE_DUPLICATE", "decisionClass": "CANONICAL_AMBIGUITY", "knownFacts": map[string]any{"amount_idr": *cmd.AmountIDR, "transaction_at": at, "merchant": merchant}, "missingFacts": []string{"duplicate_relationship"}, "allowedActions": []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}, "interactionMode": "CONFLICT_RESOLUTION"}
		encoded, _ := json.Marshal(decision)
		if _, err := tx.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3::jsonb)`, cmd.HouseholdID, result.TransactionID, string(encoded)); err != nil {
			return result, err
		}
	} else if err := RefreshOpenCycleResiduals(ctx, tx, cmd.HouseholdID, at, cmd.UserID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_proposal WHERE source_event_id=$1 AND proposal_status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END WHERE id=$1`, source); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE document SET status=CASE WHEN EXISTS(SELECT 1 FROM transaction_proposal WHERE source_event_id=$1 AND proposal_status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'EXTRACTED' END,updated_at=now() WHERE id=$2`, source, document); err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'SET_SCREENSHOT_AMOUNT','transaction',$4,jsonb_build_object('review_item_id',$5::uuid,'proposal_id',$6::uuid,'needs_duplicate_review',$7::boolean))`, cmd.HouseholdID, cmd.ActorType, cmd.UserID, result.TransactionID, cmd.ReviewItemID, cmd.ProposalID, duplicate)
	return result, err
}

// IgnoreMissingAmountProposal drops one unresolved screenshot row without
// inventing an amount. Sibling rows of the same image remain independent.
func IgnoreMissingAmountProposal(ctx context.Context, tx pgx.Tx, cmd MissingAmountCommand) error {
	var source, document string
	err := tx.QueryRow(ctx, `SELECT p.source_event_id::text,p.metadata_json->>'document_id'
		FROM review_item ri JOIN transaction_proposal p ON p.id=ri.proposal_id
		WHERE ri.id=$1 AND ri.household_id=$2 AND ri.review_type='MISSING_AMOUNT' AND ri.status IN ('OPEN','PENDING_SEND')
		AND p.id=$3 AND p.household_id=$2 AND p.amount IS NULL AND p.proposal_status='NEEDS_REVIEW'
		FOR UPDATE OF ri,p`, cmd.ReviewItemID, cmd.HouseholdID, cmd.ProposalID).Scan(&source, &document)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMissingAmountReviewInvalid
	}
	if err != nil {
		return err
	}
	if source != cmd.SourceEventID {
		return ErrMissingAmountReviewInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1`, cmd.ProposalID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='IGNORE',resolution_values='{}'::jsonb,updated_at=now() WHERE id=$1`, cmd.ReviewItemID, cmd.UserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, cmd.ReviewItemID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_proposal WHERE source_event_id=$1 AND proposal_status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END WHERE id=$1 AND household_id=$2`, source, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE document SET status=CASE WHEN EXISTS(SELECT 1 FROM transaction_proposal WHERE source_event_id=$1 AND proposal_status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'EXTRACTED' END,updated_at=now() WHERE id=$2 AND household_id=$3 AND source_event_id=$1`, source, document, cmd.HouseholdID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'IGNORE_SCREENSHOT_AMOUNT','transaction_proposal',$4,'{}'::jsonb)`, cmd.HouseholdID, cmd.ActorType, cmd.UserID, cmd.ProposalID)
	return err
}
