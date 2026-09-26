package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrPayslipReviewInvalid = errors.New("reviewdomain: payslip review is no longer valid")

type PayslipCommand struct {
	HouseholdID, UserID, ReviewItemID, ProposalID, SourceEventID, DocumentID string
	ActorType                                                                string
	Action, Choice                                                           string
	PayDate                                                                  *time.Time
}

type PayslipResult struct {
	TransactionID string
	Choice        string
}

// ResolvePayslipProposal owns the canonical proposal-to-income mutation. Surface
// code owns interaction binding and transport.
func ResolvePayslipProposal(ctx context.Context, tx pgx.Tx, cmd PayslipCommand) (PayslipResult, error) {
	var out PayslipResult
	var status, kind string
	var decision []byte
	var source, reviewSource, document, documentSource *string
	if err := tx.QueryRow(ctx, `SELECT ri.status,ri.review_type,ri.decision,p.source_event_id::text,COALESCE(ri.source_event_id,p.source_event_id)::text,COALESCE(ri.document_id,NULLIF(p.metadata_json->>'document_id','')::uuid)::text,d.source_event_id::text
		FROM review_item ri JOIN transaction_proposal p ON p.id=ri.proposal_id
		JOIN source_event s ON s.id=p.source_event_id AND s.household_id=ri.household_id
		LEFT JOIN document d ON d.id=COALESCE(ri.document_id,NULLIF(p.metadata_json->>'document_id','')::uuid) AND d.household_id=ri.household_id AND d.document_type='PAYSLIP'
		WHERE ri.id=$1 AND ri.household_id=$2 AND ri.proposal_id=$3 AND p.household_id=$2
		AND p.proposed_type='INCOME' AND p.proposal_status='NEEDS_REVIEW' FOR UPDATE OF ri,p,s`, cmd.ReviewItemID, cmd.HouseholdID, cmd.ProposalID).
		Scan(&status, &kind, &decision, &source, &reviewSource, &document, &documentSource); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrPayslipReviewInvalid
		}
		return out, err
	}
	// ponytail: legacy review items may carry no document binding at all; accept
	// that only when neither the row nor the caller supplies a document, so a real
	// mismatch still fails closed.
	documentMatches := (document == nil && cmd.DocumentID == "" && documentSource == nil) || (document != nil && *document == cmd.DocumentID && documentSource != nil && *documentSource == cmd.SourceEventID)
	if (status != "OPEN" && status != "PENDING_SEND") || source == nil || *source != cmd.SourceEventID || reviewSource == nil || *reviewSource != cmd.SourceEventID || !documentMatches || (kind != "PAYSLIP_CONFIRMATION" && kind != "MISSING_PAY_DATE") {
		return out, ErrPayslipReviewInvalid
	}
	if cmd.Action == "IGNORE" {
		var actions []string
		var decisionContract struct {
			AllowedActions []string `json:"allowedActions"`
		}
		if json.Unmarshal(decision, &decisionContract) != nil {
			return out, ErrPayslipReviewInvalid
		}
		actions = decisionContract.AllowedActions
		if !containsAction(actions, "IGNORE") {
			return out, ErrPayslipReviewInvalid
		}
		return out, ignorePayslipProposal(ctx, tx, cmd)
	}
	var contract struct {
		KnownFacts     map[string]any `json:"knownFacts"`
		MissingFacts   []string       `json:"missingFacts"`
		AllowedActions []string       `json:"allowedActions"`
	}
	if err := json.Unmarshal(decision, &contract); err != nil {
		return out, err
	}
	knownChoice, _ := contract.KnownFacts["salary_classification"].(string)
	if knownChoice != "" {
		if (knownChoice != "PRIMARY_SALARY" && knownChoice != "ORDINARY_INCOME") || (cmd.Choice != "" && cmd.Choice != knownChoice) {
			return out, ErrPayslipReviewInvalid
		}
		cmd.Choice = knownChoice
	}
	allowed := false
	for _, action := range contract.AllowedActions {
		if action == cmd.Action {
			allowed = true
			break
		}
	}
	if !allowed || (cmd.Action != "PRIMARY_SALARY" && cmd.Action != "ORDINARY_INCOME" && cmd.Action != "SET_PAY_DATE") {
		return out, ErrPayslipReviewInvalid
	}
	needsDate, needsPolicy := false, false
	for _, fact := range contract.MissingFacts {
		needsDate = needsDate || fact == "transaction_at"
		needsPolicy = needsPolicy || fact == "salary_classification"
	}
	if cmd.Action == "SET_PAY_DATE" {
		if !needsDate || cmd.PayDate == nil || cmd.PayDate.IsZero() {
			return out, ErrPayslipReviewInvalid
		}
		var hasPrimary bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)`, cmd.HouseholdID).Scan(&hasPrimary); err != nil {
			return out, err
		}
		switch {
		case knownChoice != "" && !needsPolicy && !(knownChoice == "PRIMARY_SALARY" && hasPrimary):
		case knownChoice == "" && !needsPolicy && hasPrimary && (cmd.Choice == "" || cmd.Choice == "HOUSEHOLD_POLICY"):
			cmd.Choice = "HOUSEHOLD_POLICY"
		case knownChoice == "" && !hasPrimary && needsPolicy && (cmd.Choice == "PRIMARY_SALARY" || cmd.Choice == "ORDINARY_INCOME") && containsAction(contract.AllowedActions, cmd.Choice):
		default:
			return out, ErrPayslipReviewInvalid
		}
	} else if !needsDate && needsPolicy {
		if cmd.PayDate != nil || cmd.Choice != cmd.Action {
			return out, ErrPayslipReviewInvalid
		}
		var hasPrimary bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)`, cmd.HouseholdID).Scan(&hasPrimary); err != nil {
			return out, err
		}
		if cmd.Action == "PRIMARY_SALARY" && hasPrimary {
			return out, ErrPayslipReviewInvalid
		}
	} else {
		return out, ErrPayslipReviewInvalid
	}
	var amount, employer, period string
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT amount::text,COALESCE(counterparty_raw,''),COALESCE(metadata_json->>'period',''),transaction_at
		FROM transaction_proposal WHERE id=$1 AND household_id=$2 AND proposal_status='NEEDS_REVIEW' FOR UPDATE`, cmd.ProposalID, cmd.HouseholdID).Scan(&amount, &employer, &period, &at); err != nil {
		return out, ErrPayslipReviewInvalid
	}
	if cmd.PayDate != nil {
		at = *cmd.PayDate
	}
	if cmd.Action == "SET_PAY_DATE" && cmd.PayDate == nil {
		return out, ErrPayslipReviewInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET transaction_at=$2,updated_at=now() WHERE id=$1`, cmd.ProposalID, at); err != nil {
		return out, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,counterparty_name,created_by_user_id,confirmed_at)
		VALUES($1,'INCOME','CONFIRMED',$2,'IDR',$3,'Penghasilan dari slip gaji',NULLIF($4,''),$5,now()) RETURNING id`, cmd.HouseholdID, amount, at, employer, cmd.UserID).Scan(&out.TransactionID); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'PAYSLIP_IMAGE',jsonb_strip_nulls(jsonb_build_object('proposal_id',$3::uuid,'document_id',$4::uuid)))`, out.TransactionID, cmd.SourceEventID, cmd.ProposalID, nullableUUID(cmd.DocumentID)); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='ACCEPTED',updated_at=now() WHERE id=$1`, cmd.ProposalID); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED' WHERE id=$1`, cmd.SourceEventID); err != nil {
		return out, err
	}
	if cmd.DocumentID != "" {
		if _, err := tx.Exec(ctx, `UPDATE document SET status='EXTRACTED',updated_at=now() WHERE id=$1`, cmd.DocumentID); err != nil {
			return out, err
		}
	}
	out.Choice = cmd.Choice
	if out.Choice == "PRIMARY_SALARY" || out.Choice == "HOUSEHOLD_POLICY" {
		_, err := RecordSalaryEvent(ctx, tx, SalaryCommand{HouseholdID: cmd.HouseholdID, UserID: cmd.UserID, Employer: employer, Period: period, PayDate: at, NetPay: amount, Transaction: out.TransactionID, SourceEvent: cmd.SourceEventID, MakePrimary: out.Choice == "PRIMARY_SALARY"})
		if err != nil {
			return PayslipResult{}, err
		}
	}
	resolutionValues := []byte(`{}`)
	if cmd.Action == "SET_PAY_DATE" {
		values := map[string]string{"payDate": cmd.PayDate.Format("2006-01-02")}
		if cmd.Choice == "PRIMARY_SALARY" || cmd.Choice == "ORDINARY_INCOME" {
			values["choice"] = cmd.Choice
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return PayslipResult{}, err
		}
		resolutionValues = encoded
	}
	result, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now()
		WHERE id=$1 AND household_id=$5 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, cmd.UserID, cmd.Action, string(resolutionValues), cmd.HouseholdID)
	if err != nil {
		return PayslipResult{}, err
	}
	if result.RowsAffected() != 1 {
		return PayslipResult{}, ErrPayslipReviewInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID); err != nil {
		return PayslipResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, cmd.ReviewItemID); err != nil {
		return PayslipResult{}, err
	}
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action',$5::text))`, cmd.HouseholdID, actorType, cmd.UserID, cmd.ReviewItemID, cmd.Action); err != nil {
		return PayslipResult{}, fmt.Errorf("audit payslip review resolution: %w", err)
	}
	return out, nil
}

func ignorePayslipProposal(ctx context.Context, tx pgx.Tx, cmd PayslipCommand) error {
	if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1 AND household_id=$2 AND proposal_status='NEEDS_REVIEW'`, cmd.ProposalID, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED' WHERE id=$1 AND household_id=$2`, cmd.SourceEventID, cmd.HouseholdID); err != nil {
		return err
	}
	if cmd.DocumentID != "" {
		if _, err := tx.Exec(ctx, `UPDATE document SET status='CLASSIFIED',updated_at=now() WHERE id=$1 AND household_id=$2 AND source_event_id=$3 AND document_type='PAYSLIP'`, cmd.DocumentID, cmd.HouseholdID, cmd.SourceEventID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='IGNORE',updated_at=now() WHERE id=$1 AND household_id=$3 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, cmd.UserID, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, cmd.ReviewItemID); err != nil {
		return err
	}
	actor := cmd.ActorType
	if actor == "" {
		actor = "USER"
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action','IGNORE'))`, cmd.HouseholdID, actor, cmd.UserID, cmd.ReviewItemID)
	return err
}

type PayslipPolicyCommand struct {
	HouseholdID, UserID, ActorType, ReviewItemID, ProposalID, SourceEventID, DocumentID, ReplySourceEventID, Choice string
}

// SetPayslipPolicy stores the first step of a compound payslip review as a
// canonical ReviewDecision residual, so Web and Telegram both see only the date
// still outstanding.
func SetPayslipPolicy(ctx context.Context, tx pgx.Tx, cmd PayslipPolicyCommand) error {
	if cmd.Choice != "PRIMARY_SALARY" && cmd.Choice != "ORDINARY_INCOME" {
		return ErrPayslipReviewInvalid
	}
	var status, kind string
	var decisionJSON []byte
	var source, reviewSource, document, documentSource *string
	if err := tx.QueryRow(ctx, `SELECT ri.status,ri.review_type,ri.decision,p.source_event_id::text,COALESCE(ri.source_event_id,p.source_event_id)::text,COALESCE(ri.document_id,NULLIF(p.metadata_json->>'document_id','')::uuid)::text,d.source_event_id::text
		FROM review_item ri JOIN transaction_proposal p ON p.id=ri.proposal_id
		JOIN source_event s ON s.id=p.source_event_id AND s.household_id=ri.household_id
		LEFT JOIN document d ON d.id=COALESCE(ri.document_id,NULLIF(p.metadata_json->>'document_id','')::uuid) AND d.household_id=ri.household_id AND d.document_type='PAYSLIP'
		WHERE ri.id=$1 AND ri.household_id=$2 AND ri.proposal_id=$3 AND p.household_id=$2 AND p.proposed_type='INCOME' AND p.proposal_status='NEEDS_REVIEW'
		FOR UPDATE OF ri,p,s`, cmd.ReviewItemID, cmd.HouseholdID, cmd.ProposalID).Scan(&status, &kind, &decisionJSON, &source, &reviewSource, &document, &documentSource); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPayslipReviewInvalid
		}
		return err
	}
	if (status != "OPEN" && status != "PENDING_SEND") || kind != "MISSING_PAY_DATE" || source == nil || *source != cmd.SourceEventID || reviewSource == nil || *reviewSource != cmd.SourceEventID || document == nil || *document != cmd.DocumentID || documentSource == nil || *documentSource != cmd.SourceEventID {
		return ErrPayslipReviewInvalid
	}
	var decision map[string]json.RawMessage
	if err := json.Unmarshal(decisionJSON, &decision); err != nil {
		return err
	}
	var missingFacts, allowedActions []string
	if json.Unmarshal(decision["missingFacts"], &missingFacts) != nil || json.Unmarshal(decision["allowedActions"], &allowedActions) != nil || !containsAction(missingFacts, "transaction_at") || !containsAction(missingFacts, "salary_classification") || !containsAction(allowedActions, cmd.Choice) {
		return ErrPayslipReviewInvalid
	}
	var hasPrimary bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM salary_source WHERE household_id=$1 AND active AND is_primary)`, cmd.HouseholdID).Scan(&hasPrimary); err != nil {
		return err
	}
	if hasPrimary {
		return ErrPayslipReviewInvalid
	}
	var knownFacts map[string]json.RawMessage
	if err := json.Unmarshal(decision["knownFacts"], &knownFacts); err != nil && string(decision["knownFacts"]) != "null" {
		return err
	}
	if knownFacts == nil {
		knownFacts = map[string]json.RawMessage{}
	}
	choiceJSON, _ := json.Marshal(cmd.Choice)
	knownFacts["salary_classification"] = choiceJSON
	knownJSON, err := json.Marshal(knownFacts)
	if err != nil {
		return err
	}
	remaining := make([]string, 0, len(missingFacts)-1)
	for _, fact := range missingFacts {
		if fact != "salary_classification" {
			remaining = append(remaining, fact)
		}
	}
	missingJSON, _ := json.Marshal(remaining)
	actionsJSON, _ := json.Marshal([]string{"SET_PAY_DATE", "IGNORE"})
	modeJSON, _ := json.Marshal("SINGLE_FIELD")
	decision["knownFacts"], decision["missingFacts"], decision["allowedActions"], decision["interactionMode"] = knownJSON, missingJSON, actionsJSON, modeJSON
	updatedDecision, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE review_item SET decision=$2::jsonb,updated_at=now() WHERE id=$1 AND household_id=$3 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, string(updatedDecision), cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrPayslipReviewInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='AWAITING_DATE',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND'))`, cmd.ReviewItemID); err != nil {
		return err
	}
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "TELEGRAM"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'SET_PAYSLIP_POLICY','review_item',$4,jsonb_build_object('action',$5::text))`, cmd.HouseholdID, actorType, cmd.UserID, cmd.ReviewItemID, cmd.Choice); err != nil {
		return err
	}
	result, err = tx.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,source_event_id,review_item_id,event_type,source_type,action,decision_policy_version,decision_source,bounded_choices)
		SELECT ri.household_id,se.id,ri.id,'REVIEW_TURN',se.source_type,'SET_PAYSLIP_POLICY',ri.decision->>'decisionPolicyVersion',ri.decision->>'decisionSource',1
		FROM review_item ri JOIN source_event se ON se.id=$2 AND se.household_id=ri.household_id
		WHERE ri.id=$1 AND ri.household_id=$3 AND ri.status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, cmd.ReplySourceEventID, cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrPayslipReviewInvalid
	}
	return nil
}

func containsAction(actions []string, wanted string) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

// nullableUUID lets a legacy payslip review write evidence without inventing a
// document binding it never had.
func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
