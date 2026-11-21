package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type WealthObservationCommand struct {
	HouseholdID          string
	ObservationID        string
	WealthAccountID      string
	Alias                string
	IgnoreFinancialEmail bool
	ActorUserID          string
	ActorType            string
}

var (
	ErrWealthObservationNotFound = errors.New("reviewdomain: wealth observation not found")
	ErrWealthAccountInvalid      = errors.New("reviewdomain: wealth account must be active and belong to this household")
)

func ValidateWealthAccount(ctx context.Context, tx pgx.Tx, householdID, wealthAccountID string) error {
	if strings.TrimSpace(wealthAccountID) == "" {
		return ErrWealthAccountInvalid
	}
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wealth_account WHERE id=$1 AND household_id=$2 AND active)`, wealthAccountID, householdID).Scan(&valid); err != nil {
		return invalidEntityIDError(err, ErrWealthAccountInvalid)
	}
	if !valid {
		return ErrWealthAccountInvalid
	}
	return nil
}

func ResolveWealthObservation(ctx context.Context, tx pgx.Tx, cmd WealthObservationCommand) error {
	if err := ValidateWealthAccount(ctx, tx, cmd.HouseholdID, cmd.WealthAccountID); err != nil {
		return err
	}
	var hint string
	if err := tx.QueryRow(ctx, `SELECT account_hint FROM wealth_observation WHERE id=$1 AND household_id=$2 AND status='PENDING' FOR UPDATE`, cmd.ObservationID, cmd.HouseholdID).Scan(&hint); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWealthObservationNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE wealth_observation SET resolved_wealth_account_id=$2,status='ACCEPTED',updated_at=now() WHERE id=$1 AND household_id=$3`, cmd.ObservationID, cmd.WealthAccountID, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='APPLIED',updated_at=now() WHERE household_id=$2 AND id=(SELECT financial_email_observation_id FROM wealth_observation WHERE id=$1 AND household_id=$2) AND status IN ('PENDING','REVIEW')`, cmd.ObservationID, cmd.HouseholdID); err != nil {
		return err
	}
	if cmd.Alias != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,wealth_account_id,alias,normalized_alias,source) VALUES($1,'WEALTH_ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET wealth_account_id=EXCLUDED.wealth_account_id,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now() WHERE financial_entity_alias.source <> 'USER'`, cmd.HouseholdID, cmd.WealthAccountID, strings.TrimSpace(hint), cmd.Alias); err != nil {
			return err
		}
	}
	return completeWealthReview(ctx, tx, cmd, "SET_WEALTH_ACCOUNT", map[string]any{"wealthAccountId": cmd.WealthAccountID, "pending_snapshot": true})
}

func completeWealthReview(ctx context.Context, tx pgx.Tx, cmd WealthObservationCommand, action string, values map[string]any) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=NULLIF($2,'')::uuid,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now()
		WHERE wealth_observation_id=$1 AND household_id=$5 AND status IN ('OPEN','PENDING_SEND') RETURNING id::text`, cmd.ObservationID, cmd.ActorUserID, action, string(encoded), cmd.HouseholdID)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, resolveRequestByItemSQL, items, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=ANY($1::uuid[]))`, items); err != nil {
		return err
	}
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,NULLIF($3,'')::uuid,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action',$5::text,'wealth_observation_id',$6::uuid))`, cmd.HouseholdID, actorType, cmd.ActorUserID, item, action, cmd.ObservationID); err != nil {
			return err
		}
	}
	return RefreshWealthObservationSource(ctx, tx, cmd.HouseholdID, cmd.ObservationID)
}

func RefreshWealthObservationSource(ctx context.Context, tx pgx.Tx, householdID, observationID string) error {
	var source string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT d.source_event_id::text FROM document d WHERE d.id=wo.document_id AND d.household_id=wo.household_id),(SELECT f.source_event_id::text FROM financial_email_observation f WHERE f.id=wo.financial_email_observation_id AND f.household_id=wo.household_id),'') FROM wealth_observation wo WHERE wo.id=$1 AND wo.household_id=$2`, observationID, householdID).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && source == "") {
		return nil
	}
	if err != nil {
		return err
	}
	return RefreshSourceLifecycle(ctx, tx, householdID, source)
}

func DismissWealthObservation(ctx context.Context, tx pgx.Tx, cmd WealthObservationCommand) error {
	result, err := tx.Exec(ctx, `UPDATE wealth_observation SET status='DISMISSED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status='PENDING'`, cmd.ObservationID, cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrWealthObservationNotFound
	}
	if cmd.IgnoreFinancialEmail {
		if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE household_id=$2 AND id=(SELECT financial_email_observation_id FROM wealth_observation WHERE id=$1 AND household_id=$2)`, cmd.ObservationID, cmd.HouseholdID); err != nil {
			return err
		}
	}
	return nil
}

func ReclassifyWealthEvidence(ctx context.Context, tx pgx.Tx, householdID, observationID string) error {
	_, err := tx.Exec(ctx, `UPDATE document SET document_type='TRANSACTION_HISTORY_SCREENSHOT',status='EXTRACTED',updated_at=now() WHERE household_id=$2 AND id=(SELECT document_id FROM wealth_observation WHERE id=$1 AND household_id=$2)`, observationID, householdID)
	return err
}
