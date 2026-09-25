// Wealth observation resolution (UIR-01). The Review Inbox and the Telegram
// review lanes all mutate wealth_observation the same way; this operation owns
// those mutations so the Web and Telegram paths cannot drift.
package reviewdomain

import (
	"context"
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
		return err
	}
	if !valid {
		return ErrWealthAccountInvalid
	}
	return nil
}

// ResolveWealthObservation records the chosen wealth account on a pending
// observation and learns the observed hint as a household-scoped alias. It
// returns ErrWealthObservationNotFound when a concurrent decision already
// dismissed or resolved the observation, which surfaces report as stale.
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
	if _, err := tx.Exec(ctx, `UPDATE wealth_observation SET resolved_wealth_account_id=$2,updated_at=now() WHERE id=$1 AND household_id=$3`, cmd.ObservationID, cmd.WealthAccountID, cmd.HouseholdID); err != nil {
		return err
	}
	if cmd.Alias != "" {
		// A user-authored alias is never overwritten by review learning.
		if _, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,wealth_account_id,alias,normalized_alias,source) VALUES($1,'WEALTH_ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET wealth_account_id=EXCLUDED.wealth_account_id,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now() WHERE financial_entity_alias.source <> 'USER'`, cmd.HouseholdID, cmd.WealthAccountID, strings.TrimSpace(hint), cmd.Alias); err != nil {
			return err
		}
	}
	return nil
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
		if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=(SELECT financial_email_observation_id FROM wealth_observation WHERE id=$1)`, cmd.ObservationID); err != nil {
			return err
		}
	}
	return nil
}

// ReclassifyWealthEvidence marks the document behind a wealth observation as a
// transaction-history screenshot once the observation became an asset purchase,
// keeping the evidence provenance recorded.
func ReclassifyWealthEvidence(ctx context.Context, tx pgx.Tx, observationID string) error {
	_, err := tx.Exec(ctx, `UPDATE document SET document_type='TRANSACTION_HISTORY_SCREENSHOT',status='EXTRACTED',updated_at=now() WHERE id=(SELECT document_id FROM wealth_observation WHERE id=$1)`, observationID)
	return err
}
