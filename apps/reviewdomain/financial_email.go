// Financial email entity resolution (UIR-01). The Review Inbox resolves a
// financial email observation by supplying only the entities that are still
// unresolved; this operation owns that merge, validation, alias learning, and
// replay enqueue so the resolved-fact rules cannot drift per channel.
package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/jackc/pgx/v5"
)

// FinancialEmailCommand carries one entity resolution for a financial email
// observation. Empty entity values mean "not supplied this turn"; already
// resolved entities are reloaded from persisted state.
type FinancialEmailCommand struct {
	HouseholdID   string
	ObservationID string
	// ObservationScope is the household-scoped observation update; it is not
	// optional because a resolution without a bound observation is refused.
	AccountID       string
	WealthAccountID string
	// AccountHint and ProviderHint are the raw hints to learn as aliases.
	AccountHint  string
	ProviderHint string
	// SourceEventID is the email source to requeue once the entities resolve.
	SourceEventID string
	ActorUserID   string
}

// FinancialEmailResult reports the resolved entity binding so each surface can
// record its own audit and response shape.
type FinancialEmailResult struct {
	// ObservationID is the canonical observation the resolution applied to.
	ObservationID   string
	AccountID       string
	WealthAccountID string
	// HumanSupplied lists the entities supplied by this request, which callers
	// use for RHICE attribution. Client telemetry claims are never trusted.
	HumanSupplied []string
	// Values is the canonical resolution payload stored on the review item.
	Values []byte
}

var (
	// ErrFinancialObservationUnavailable reports a missing or non-review observation.
	ErrFinancialObservationUnavailable = errors.New("reviewdomain: financial observation is unavailable")
	// ErrFundingAccountRequired reports a resolution that leaves the funding account blank.
	ErrFundingAccountRequired = errors.New("reviewdomain: the unresolved funding account is required")
	// ErrProviderAccountRequired reports a resolution that leaves the Wealth Account blank.
	ErrProviderAccountRequired = errors.New("reviewdomain: the unresolved Wealth Account is required")
	// ErrAccountInvalid reports an inactive or foreign funding account.
	ErrAccountInvalid = errors.New("reviewdomain: account must be active and belong to this household")
)

// ResolveFinancialEmailEntities merges the supplied entities with the entities
// already persisted on the observation, validates them against the household,
// stores them, learns aliases for newly resolved entities, and returns the
// canonical binding. It does not complete the review item or enqueue replay:
// those remain the caller's terminal steps so each surface keeps its own
// transport and job contract.
func ResolveFinancialEmailEntities(ctx context.Context, tx pgx.Tx, cmd FinancialEmailCommand) (FinancialEmailResult, error) {
	var result FinancialEmailResult
	var observationID, fundingHint, providerHint, knownAccount, knownWealth string
	err := tx.QueryRow(ctx, `SELECT id::text,COALESCE(facts_json->>'funding_account_hint',''),COALESCE(facts_json->>'provider_account_hint',''),COALESCE(resolved_account_id::text,''),COALESCE(resolved_wealth_account_id::text,'') FROM financial_email_observation WHERE id=$1 AND household_id=$2 AND status='REVIEW' FOR UPDATE`, cmd.ObservationID, cmd.HouseholdID).Scan(&observationID, &fundingHint, &providerHint, &knownAccount, &knownWealth)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrFinancialObservationUnavailable
	}
	if err != nil {
		return result, err
	}
	// PRD §12/§20.1: the request supplies only the unresolved entities. Already
	// resolved entities are reloaded from persisted state and merged here, so a
	// review that already knows the funding account never asks for it again. An
	// entity that is still unresolved must be supplied: a partial resolution that
	// leaves one blank would write a half-bound observation.
	accountID, wealthAccountID := strings.TrimSpace(cmd.AccountID), strings.TrimSpace(cmd.WealthAccountID)
	if knownAccount == "" && accountID == "" {
		return result, ErrFundingAccountRequired
	}
	if knownWealth == "" && wealthAccountID == "" {
		return result, ErrProviderAccountRequired
	}
	if accountID == "" {
		accountID = knownAccount
	}
	if wealthAccountID == "" {
		wealthAccountID = knownWealth
	}
	if accountID != "" {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account WHERE id=$1 AND household_id=$2 AND active)`, accountID, cmd.HouseholdID).Scan(&valid); err != nil {
			return result, err
		}
		if !valid {
			return result, ErrAccountInvalid
		}
	}
	if wealthAccountID != "" {
		if err := ValidateWealthAccount(ctx, tx, cmd.HouseholdID, wealthAccountID); err != nil {
			return result, err
		}
	}
	if cmd.AccountID != "" {
		result.HumanSupplied = append(result.HumanSupplied, "account")
	}
	if cmd.WealthAccountID != "" {
		result.HumanSupplied = append(result.HumanSupplied, "wealth_account")
	}
	if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET resolved_account_id=NULLIF($2,'')::uuid,resolved_wealth_account_id=NULLIF($3,'')::uuid,status='PENDING',updated_at=now() WHERE id=$1`, observationID, accountID, wealthAccountID); err != nil {
		return result, err
	}
	// An entity that was already known keeps its existing alias: re-learning here
	// would let a partial resolution rewrite an established mapping.
	if err := LearnEntityAliasIfNew(ctx, tx, cmd.HouseholdID, "ACCOUNT", accountID, fundingHint, knownAccount); err != nil {
		return result, err
	}
	if err := LearnEntityAliasIfNew(ctx, tx, cmd.HouseholdID, "WEALTH_ACCOUNT", wealthAccountID, providerHint, knownWealth); err != nil {
		return result, err
	}
	payload, err := json.Marshal(struct {
		AccountID       string   `json:"accountId"`
		WealthAccountID string   `json:"wealthAccountId"`
		HumanSupplied   []string `json:"human_supplied_fields,omitempty"`
	}{AccountID: accountID, WealthAccountID: wealthAccountID, HumanSupplied: result.HumanSupplied})
	if err != nil {
		return result, err
	}
	result.ObservationID = observationID
	result.AccountID, result.WealthAccountID, result.Values = accountID, wealthAccountID, payload
	return result, nil
}

// LearnEntityAlias records the alias a user supplied for an entity, refusing to
// overwrite an alias the household set itself: an explicit user choice outranks
// an inferred one (PRD §19). entityType is ACCOUNT or WEALTH_ACCOUNT.
func LearnEntityAlias(ctx context.Context, tx pgx.Tx, householdID, entityType, entityID, alias string) error {
	normalized := normalizeAlias(alias)
	if normalized == "" {
		return nil
	}
	if entityType == "ACCOUNT" {
		_, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,account_id,alias,normalized_alias,source) VALUES($1,'ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET account_id=EXCLUDED.account_id,wealth_account_id=NULL,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now() WHERE financial_entity_alias.source <> 'USER'`, householdID, entityID, strings.TrimSpace(alias), normalized)
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO financial_entity_alias(household_id,entity_type,wealth_account_id,alias,normalized_alias,source) VALUES($1,'WEALTH_ACCOUNT',$2,$3,$4,'REVIEW_LEARNED') ON CONFLICT (household_id,entity_type,normalized_alias) WHERE active DO UPDATE SET wealth_account_id=EXCLUDED.wealth_account_id,account_id=NULL,alias=EXCLUDED.alias,source='REVIEW_LEARNED',updated_at=now() WHERE financial_entity_alias.source <> 'USER'`, householdID, entityID, strings.TrimSpace(alias), normalized)
	return err
}

// LearnEntityAliasIfNew learns an alias only when the entity was not already
// known before this request. An empty known entity means the alias is new.
func LearnEntityAliasIfNew(ctx context.Context, tx pgx.Tx, householdID, entityType, entityID, alias, known string) error {
	if strings.TrimSpace(known) != "" {
		return nil
	}
	return LearnEntityAlias(ctx, tx, householdID, entityType, entityID, alias)
}

// normalizeAlias folds an alias into its stored comparable form: NFKC
// compatibility normalization, lower case, letters and digits only,
// single-spaced. Every surface stores this one key, so the fold lives here
// rather than at each call site.
func normalizeAlias(value string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(norm.NFKC.String(value))) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		default:
			space = true
		}
	}
	return b.String()
}
