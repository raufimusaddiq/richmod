package reviewdomain

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrEvidenceDisputeInvalid = errors.New("reviewdomain: screenshot dispute needs one current merged row with a known amount and date")

var ErrEvidenceDisputeConfirmationRequired = errors.New("reviewdomain: use the screenshot dispute confirmation buttons")

type EvidenceDisputeCommand struct {
	HouseholdID, ActorUserID, DocumentID, SourceEventID string
	TransactionID, ReviewItemID, RequestID              string
}

type EvidenceDisputeResult struct {
	TransactionID string
	Created       bool
	Status        string
}

type mergedScreenshot struct {
	proposal, source, evidence, original, kind, amount, currency, merchant, description string
	category                                                                            *string
	at                                                                                  time.Time
}

// Lock the original observation, not a model-selected ledger target. Identity is
// deliberately not guessed for legacy extractions; only human confirmation can
// override the old match. Missing amount/date and multirow documents fail closed.
func lockMergedScreenshot(ctx context.Context, tx pgx.Tx, household, document string) (mergedScreenshot, error) {
	var s mergedScreenshot
	err := tx.QueryRow(ctx, `SELECT p.id::text,p.source_event_id::text,p.proposed_type,p.amount::text,p.currency,p.transaction_at,COALESCE(p.merchant_raw,p.counterparty_raw,''),COALESCE(p.description,''),p.category_candidate_id
		FROM document d JOIN source_event se ON se.id=d.source_event_id AND se.household_id=d.household_id
		JOIN transaction_proposal p ON p.source_event_id=se.id AND p.household_id=d.household_id
		WHERE d.id=$1::uuid AND d.household_id=$2 AND se.source_type='TELEGRAM_IMAGE'
		AND d.document_type IN ('BANK_TRANSACTION_SCREENSHOT','EWALLET_SCREENSHOT','TRANSACTION_HISTORY_SCREENSHOT','TRANSFER_PROOF')
		AND p.metadata_json->>'document_id'=d.id::text
		AND (p.metadata_json->>'date_known'='true' OR EXISTS(SELECT 1 FROM document_extraction de WHERE de.document_id=d.id AND de.stage='TRANSACTION_SCREENSHOT' AND de.validated
		AND jsonb_array_length(de.output_json->'transactions')=1 AND de.output_json->'transactions'->0->>'transaction_at' IS NOT NULL))
		AND p.proposal_status='MERGED' AND p.amount>0 AND p.currency='IDR' AND p.proposed_type IN ('EXPENSE','INCOME')
		AND (SELECT count(*) FROM transaction_proposal other WHERE other.source_event_id=se.id)=1
		AND (SELECT count(*) FROM document_page dp WHERE dp.document_id=d.id AND dp.source_event_id<>se.id)=0
		FOR UPDATE OF d,p`, document, household).Scan(&s.proposal, &s.source, &s.kind, &s.amount, &s.currency, &s.at, &s.merchant, &s.description, &s.category)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrEvidenceDisputeInvalid
	}
	if err != nil {
		return s, err
	}
	err = tx.QueryRow(ctx, `SELECT te.id::text,t.id::text FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id
		JOIN transaction_proposal p ON p.id=$3::uuid
		WHERE te.source_event_id=$1::uuid AND t.household_id=$2 AND t.status='CONFIRMED'
		AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE'
		AND te.metadata_json->>'proposal_id'=p.id::text AND COALESCE(p.metadata_json->>'matched_transaction_id',p.metadata_json->>'merged_into')=t.id::text
		AND (SELECT count(*) FROM transaction_evidence e JOIN transaction linked ON linked.id=e.transaction_id
		WHERE e.source_event_id=$1::uuid AND linked.status<>'VOIDED' AND COALESCE(e.metadata_json->>'link_status','ACTIVE')='ACTIVE')=1
		FOR UPDATE OF te,t`, s.source, household, s.proposal).Scan(&s.evidence, &s.original)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrEvidenceDisputeInvalid
	}
	return s, err
}

func StageEvidenceDispute(ctx context.Context, tx pgx.Tx, cmd EvidenceDisputeCommand) (EvidenceDisputeResult, error) {
	var result EvidenceDisputeResult
	s, err := lockMergedScreenshot(ctx, tx, cmd.HouseholdID, cmd.DocumentID)
	if err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `SELECT t.id::text,t.status FROM transaction_evidence e JOIN transaction t ON t.id=e.transaction_id
		WHERE t.household_id=$1 AND e.metadata_json->>'original_evidence_id'=$2 AND e.metadata_json->>'evidence_link_dispute'='true'
		ORDER BY e.created_at DESC LIMIT 1`, cmd.HouseholdID, s.evidence).Scan(&result.TransactionID, &result.Status)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,counterparty_name,description,created_by_user_id)
		VALUES($1,$2,'NEEDS_REVIEW',$3::numeric,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9) RETURNING id::text`, cmd.HouseholdID, s.kind, s.amount, s.currency, s.at, s.category, s.merchant, s.description, cmd.ActorUserID).Scan(&result.TransactionID)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json)
		VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('evidence_link_dispute',true,'original_proposal_id',$3::text,'original_evidence_id',$4::text,'original_transaction_id',$5::text,'disputed_document_id',$6::text))`, result.TransactionID, cmd.SourceEventID, s.proposal, s.evidence, s.original, cmd.DocumentID)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json)
		VALUES($1,'TELEGRAM',$2,'STAGE_EVIDENCE_DISPUTE','transaction',$3,jsonb_build_object('original_evidence_id',$4::uuid,'original_transaction_id',$5::uuid,'document_id',$6::uuid))`, cmd.HouseholdID, cmd.ActorUserID, result.TransactionID, s.evidence, s.original, cmd.DocumentID)
	result.Created, result.Status = true, "NEEDS_REVIEW"
	return result, err
}

// ResolveEvidenceDispute is intentionally separate from generic duplicate
// confirm/merge: the original immutable proposal remains MERGED forever.
func ResolveEvidenceDispute(ctx context.Context, tx pgx.Tx, cmd EvidenceDisputeCommand, confirm bool) error {
	var originalEvidence, originalProposal, document, disputeSource string
	err := tx.QueryRow(ctx, `SELECT e.metadata_json->>'original_evidence_id',e.metadata_json->>'original_proposal_id',e.metadata_json->>'disputed_document_id',e.source_event_id::text
		FROM review_item ri JOIN transaction t ON t.id=ri.transaction_id AND t.household_id=ri.household_id
		JOIN transaction_evidence e ON e.transaction_id=t.id
		WHERE ri.id=$1::uuid AND ri.household_id=$2 AND ri.transaction_id=$3::uuid AND ri.status IN ('OPEN','PENDING_SEND')
		AND t.status='NEEDS_REVIEW' AND e.metadata_json->>'evidence_link_dispute'='true'
		FOR UPDATE OF ri,t,e`, cmd.ReviewItemID, cmd.HouseholdID, cmd.TransactionID).Scan(&originalEvidence, &originalProposal, &document, &disputeSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	if err != nil {
		return err
	}
	action := "REJECT_EVIDENCE_DISPUTE"
	if confirm {
		s, err := lockMergedScreenshot(ctx, tx, cmd.HouseholdID, document)
		if err != nil {
			return err
		}
		if s.evidence != originalEvidence || s.proposal != originalProposal {
			return ErrEvidenceDisputeInvalid
		}
		var recoveryProposal string
		err = tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposal_key,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,confidence,proposal_status,metadata_json)
			SELECT household_id,source_event_id,'dispute:'||id::text,proposed_type,amount,currency,transaction_at,merchant_raw,category_candidate_id,description,confidence,'ACCEPTED',(metadata_json-'matched_transaction_id'-'merged_into'-'match_score')||jsonb_build_object('original_proposal_id',id::text,'evidence_link_dispute',true,'date_known',true,'date_source','ORIGINAL_OBSERVATION') FROM transaction_proposal WHERE id=$1::uuid RETURNING id::text`, s.proposal).Scan(&recoveryProposal)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE transaction_evidence SET metadata_json=metadata_json||jsonb_build_object('link_status','SUPERSEDED','superseded_by_transaction_id',$2::text,'superseded_by_review_item_id',$3::text) WHERE id=$1::uuid`, s.evidence, cmd.TransactionID, cmd.ReviewItemID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json)
			SELECT $2::uuid,source_event_id,evidence_type,confidence,(metadata_json-'link_status'-'superseded_by_transaction_id'-'superseded_by_review_item_id')||jsonb_build_object('proposal_id',$3::text,'original_proposal_id',$4::text,'recovered_from_evidence_id',id::text,'document_id',$5::text)
			FROM transaction_evidence WHERE id=$1::uuid`, s.evidence, cmd.TransactionID, recoveryProposal, s.proposal, document); err != nil {
			return err
		}
		if _, err = ConfirmTransactionReview(ctx, tx, ConfirmCommand{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, TransactionID: cmd.TransactionID, ReviewItemID: cmd.ReviewItemID, RequestID: cmd.RequestID, ReviewType: "POSSIBLE_DUPLICATE", Action: "CONFIRM_EVIDENCE_DISPUTE", ResolveReview: true}); err != nil {
			return err
		}
		action = "CONFIRM_EVIDENCE_DISPUTE"
	} else {
		if _, err = tx.Exec(ctx, `UPDATE transaction SET status='VOIDED',voided_at=now(),updated_at=now() WHERE id=$1::uuid`, cmd.TransactionID); err != nil {
			return err
		}
		if err = ResolveByID(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, ReviewItemID: cmd.ReviewItemID, RequestID: cmd.RequestID, SubjectID: cmd.TransactionID, Action: action}); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id=$1::uuid`, cmd.RequestID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json)
		VALUES($1,'TELEGRAM',$2,$3,'transaction',$4,jsonb_build_object('original_proposal_id',$5::uuid,'original_evidence_id',$6::uuid,'document_id',$7::uuid,'review_item_id',$8::uuid))`, cmd.HouseholdID, cmd.ActorUserID, action, cmd.TransactionID, originalProposal, originalEvidence, document, cmd.ReviewItemID)
	return err
}
