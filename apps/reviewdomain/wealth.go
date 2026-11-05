// Wealth observation resolution. The Review Inbox and the Telegram
// review lanes all mutate wealth_observation the same way; this operation owns
// those mutations so the Web and Telegram paths cannot drift.
package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// WealthObservationCommand pins the observation under review and its actor.
type WealthObservationCommand struct {
	HouseholdID   string
	ObservationID string
	// WealthAccountID is the account a SET_WEALTH_ACCOUNT decision selected.
	WealthAccountID string
	// Alias is the already-normalized observed hint to learn, or empty to learn
	// nothing. Callers normalize it because folding is a presentation concern the
	// adapters already own.
	Alias string
	// IgnoreFinancialEmail also marks the linked financial email observation
	// ignored, which only the Review Inbox IGNORE action does.
	IgnoreFinancialEmail bool
	// ActorUserID is the authenticated household member completing the review,
	// or empty when the surface resolves through a bound projection only.
	ActorUserID string
	// ActorType is the audit surface ('USER' for Web, 'TELEGRAM' for chat).
	ActorType string
}

var (
	// ErrWealthObservationNotFound reports an observation outside this household
	// or one that is no longer pending.
	ErrWealthObservationNotFound = errors.New("reviewdomain: wealth observation not found")
	// ErrWealthAccountInvalid reports an inactive or foreign wealth account.
	ErrWealthAccountInvalid = errors.New("reviewdomain: wealth account must be active and belong to this household")
)

// ValidateWealthAccount confirms the account is active and household-scoped.
// Web and Telegram both performed this check inline before resolving.
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

// ResolveWealthObservation records the chosen wealth account on a pending
// observation, accepts it, and completes its review in the same transaction
// (ADR-036 amendment, 2026-11): choosing the account is the one household
// decision the review asks for. The accepted observation stays an observation:
// it is listed on /wealth as a value waiting for the next complete snapshot and
// never becomes a transaction or a partial snapshot. The observed hint is
// learned as a household-scoped alias. It returns ErrWealthObservationNotFound
// when a concurrent decision already dismissed or resolved the observation,
// which surfaces report as stale.
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
	// The provider email behind the value is applied exactly as an observation
	// that resolved its account deterministically would be.
	if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='APPLIED',updated_at=now() WHERE household_id=$2 AND id=(SELECT financial_email_observation_id FROM wealth_observation WHERE id=$1 AND household_id=$2) AND status IN ('PENDING','REVIEW')`, cmd.ObservationID, cmd.HouseholdID); err != nil {
		return err
	}
	if cmd.Alias != "" {
		// A user-authored alias is never overwritten by review learning.
		if _, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,wealth_account_id,alias,normalized_alias,source) VALUES($1,'WEALTH_ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET wealth_account_id=EXCLUDED.wealth_account_id,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now() WHERE financial_entity_alias.source <> 'USER'`, cmd.HouseholdID, cmd.WealthAccountID, strings.TrimSpace(hint), cmd.Alias); err != nil {
			return err
		}
	}
	return completeWealthReview(ctx, tx, cmd, "SET_WEALTH_ACCOUNT", map[string]any{"wealthAccountId": cmd.WealthAccountID, "pending_snapshot": true})
}

// completeWealthReview resolves every open review bound to the observation and
// its Telegram projection, audits the decision, and settles the evidence source
// through the shared lifecycle.
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

// RefreshWealthObservationSource settles the evidence source behind one wealth
// observation (the document's source or the financial email's source).
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

// DismissWealthObservation closes a pending observation, returning
// ErrWealthObservationNotFound when a concurrent decision already closed it.
//
// IgnoreFinancialEmail controls whether the financial email observation behind
// this wealth observation is also marked ignored. The Review Inbox IGNORE action
// does that; the asset-purchase reclassification path deliberately does not,
// because the evidence became a transaction rather than being discarded.
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

// ReclassifyWealthEvidence marks the document behind a wealth observation as a
// transaction-history screenshot once the observation became an asset purchase,
// keeping the evidence provenance recorded.
func ReclassifyWealthEvidence(ctx context.Context, tx pgx.Tx, householdID, observationID string) error {
	_, err := tx.Exec(ctx, `UPDATE document SET document_type='TRANSACTION_HISTORY_SCREENSHOT',status='EXTRACTED',updated_at=now() WHERE household_id=$2 AND id=(SELECT document_id FROM wealth_observation WHERE id=$1 AND household_id=$2)`, observationID, householdID)
	return err
}
