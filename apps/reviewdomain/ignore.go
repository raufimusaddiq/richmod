package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrUseTransactionAction reports an IGNORE against a transaction review that
// still awaits its decision: the transaction lane (confirm/reject/merge) owns it.
var ErrUseTransactionAction = errors.New("reviewdomain: use the transaction review action")

// IgnoreCommand dismisses or acknowledges one review item. It is the generic
// IGNORE every surface shares (ADR-046): the lane-specific ignores (bank facts,
// financial email, transfer, payslip, missing amount, document) keep their own
// operations; this one covers everything else.
type IgnoreCommand struct {
	HouseholdID  string
	ActorUserID  string
	ActorType    string
	ReviewItemID string
	Values       []byte
}

// IgnoreReviewItem resolves one open review with IGNORE semantics:
//
//   - a transaction-bound review whose transaction is already final is only
//     acknowledged (NO_LONGER_APPLICABLE); the transaction is untouched;
//   - otherwise the bound evidence is dismissed without deleting it (financial
//     email observation IGNORED, wealth observation DISMISSED, proposal
//     REJECTED), the review and its Telegram projection are resolved, and the
//     source is settled by the shared lifecycle (MarkSourceIgnored).
func IgnoreReviewItem(ctx context.Context, tx pgx.Tx, cmd IgnoreCommand) error {
	var status string
	var proposal, source, document, transaction, residual, wealth, financial *string
	err := tx.QueryRow(ctx, `SELECT status,proposal_id::text,source_event_id::text,document_id::text,transaction_id::text,cycle_residual_case_id::text,wealth_observation_id::text,financial_email_observation_id::text FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, cmd.ReviewItemID, cmd.HouseholdID).Scan(&status, &proposal, &source, &document, &transaction, &residual, &wealth, &financial)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	if err != nil {
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return ErrAlreadyResolved
	}
	if residual != nil {
		return ErrUseTransactionAction
	}
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if transaction != nil {
		var txStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1 AND household_id=$2`, *transaction, cmd.HouseholdID).Scan(&txStatus); err != nil {
			return err
		}
		if txStatus == "NEEDS_REVIEW" {
			return ErrUseTransactionAction
		}
		if err := resolveItem(ctx, tx, cmd, "NO_LONGER_APPLICABLE", []byte(`{"reason":"transaction_already_final"}`)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,NULLIF($3,'')::uuid,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action','IGNORE','reason','transaction_already_final'))`, cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.ReviewItemID)
		return err
	}
	if financial != nil {
		if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status IN ('PENDING','REVIEW')`, *financial, cmd.HouseholdID); err != nil {
			return err
		}
	}
	if wealth != nil {
		if err := DismissWealthObservation(ctx, tx, WealthObservationCommand{HouseholdID: cmd.HouseholdID, ObservationID: *wealth, IgnoreFinancialEmail: true}); err != nil && !errors.Is(err, ErrWealthObservationNotFound) {
			return err
		}
	}
	if proposal != nil {
		if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1 AND household_id=$2 AND proposal_status='NEEDS_REVIEW'`, *proposal, cmd.HouseholdID); err != nil {
			return err
		}
	}
	values := cmd.Values
	if len(values) == 0 {
		values = []byte(`{}`)
	}
	if err := resolveItem(ctx, tx, cmd, "IGNORE", values); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,NULLIF($3,'')::uuid,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action','IGNORE'))`, cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.ReviewItemID); err != nil {
		return err
	}
	sources, err := reviewItemSources(ctx, tx, cmd.HouseholdID, proposal, source, document, wealth, financial)
	if err != nil {
		return err
	}
	for _, id := range sources {
		if err := MarkSourceIgnored(ctx, tx, cmd.HouseholdID, id); err != nil {
			return err
		}
	}
	return nil
}

func resolveItem(ctx context.Context, tx pgx.Tx, cmd IgnoreCommand, action string, values []byte) error {
	if !json.Valid(values) {
		values = []byte(`{}`)
	}
	result, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=NULLIF($2,'')::uuid,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1 AND household_id=$5 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.ActorUserID, action, string(values), cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, resolveRequestByItemSQL, []string{cmd.ReviewItemID}, cmd.HouseholdID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1 AND household_id=$2)`, cmd.ReviewItemID, cmd.HouseholdID)
	return err
}

// reviewItemSources returns the evidence source events one review is about.
func reviewItemSources(ctx context.Context, tx pgx.Tx, householdID string, proposal, source, document, wealth, financial *string) ([]string, error) {
	var out []string
	add := func(id string) {
		if id == "" {
			return
		}
		for _, existing := range out {
			if existing == id {
				return
			}
		}
		out = append(out, id)
	}
	if source != nil {
		add(*source)
	}
	lookups := []struct {
		id  *string
		sql string
	}{
		{proposal, `SELECT source_event_id::text FROM transaction_proposal WHERE id=$1 AND household_id=$2`},
		{document, `SELECT source_event_id::text FROM document WHERE id=$1 AND household_id=$2`},
		{financial, `SELECT source_event_id::text FROM financial_email_observation WHERE id=$1 AND household_id=$2`},
		{wealth, `SELECT COALESCE((SELECT d.source_event_id::text FROM document d WHERE d.id=wo.document_id),(SELECT f.source_event_id::text FROM financial_email_observation f WHERE f.id=wo.financial_email_observation_id),'') FROM wealth_observation wo WHERE wo.id=$1 AND wo.household_id=$2`},
	}
	for _, lookup := range lookups {
		if lookup.id == nil {
			continue
		}
		var id string
		if err := tx.QueryRow(ctx, lookup.sql, *lookup.id, householdID).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		add(id)
	}
	return out, nil
}
