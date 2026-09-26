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

	"github.com/jackc/pgx/v5/pgconn"
)

// FinancialEmailCommand carries one entity resolution for a financial email
// observation. Empty entity values mean "not supplied this turn"; already
// resolved entities are reloaded from persisted state.
type FinancialEmailCommand struct {
	HouseholdID   string
	ObservationID string
	ReviewItemID  string
	Ignore        bool
	AllowPartial  bool
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
	Complete bool
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
	if knownAccount == "" && accountID == "" && !cmd.AllowPartial {
		return result, ErrFundingAccountRequired
	}
	if knownWealth == "" && wealthAccountID == "" && !cmd.AllowPartial {
		return result, ErrProviderAccountRequired
	}
	if accountID == "" {
		accountID = knownAccount
	}
	if wealthAccountID == "" {
		wealthAccountID = knownWealth
	}
	if cmd.AllowPartial && cmd.AccountID == "" && cmd.WealthAccountID == "" {
		return result, ErrFundingAccountRequired
	}
	if accountID != "" {
		if err := ValidateFinancialFundingAccount(ctx, tx, cmd.HouseholdID, accountID); err != nil {
			return result, err
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
	result.Complete = accountID != "" && wealthAccountID != ""
	if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET resolved_account_id=NULLIF($2,'')::uuid,resolved_wealth_account_id=NULLIF($3,'')::uuid,status=CASE WHEN $4::boolean THEN 'PENDING' ELSE 'REVIEW' END,updated_at=now() WHERE id=$1`, observationID, accountID, wealthAccountID, result.Complete); err != nil {
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

// ResolveFinancialEmailReview owns partial/terminal entity binding and ignore.
// All surfaces pass a household-scoped review ID and authenticated actor.
func ResolveFinancialEmailReview(ctx context.Context, tx pgx.Tx, cmd FinancialEmailCommand) (FinancialEmailResult, error) {
	var result FinancialEmailResult
	var sourceID string
	err := tx.QueryRow(ctx, `SELECT fo.source_event_id::text FROM review_item ri JOIN financial_email_observation fo ON fo.id=ri.financial_email_observation_id AND fo.household_id=ri.household_id WHERE ri.id=$1 AND ri.household_id=$2 AND ri.status IN ('OPEN','PENDING_SEND') AND ri.review_type='FINANCIAL_EMAIL_RESOLUTION' AND fo.id=$3 AND fo.status='REVIEW' FOR UPDATE OF ri,fo`, cmd.ReviewItemID, cmd.HouseholdID, cmd.ObservationID).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrFinancialObservationUnavailable
	}
	if err != nil {
		return result, err
	}
	if cmd.Ignore {
		if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2`, cmd.ObservationID, cmd.HouseholdID); err != nil {
			return result, err
		}
		result.Complete = true
	} else {
		cmd.AllowPartial = true
		result, err = ResolveFinancialEmailEntities(ctx, tx, cmd)
		if err != nil || !result.Complete {
			return result, err
		}
	}
	action := "SET_FINANCIAL_EMAIL_ENTITIES"
	if cmd.Ignore {
		action = "IGNORE"
	}
	values := "{}"
	if len(result.Values) > 0 {
		values = string(result.Values)
	}
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1 AND household_id=$5`, cmd.ReviewItemID, cmd.ActorUserID, action, values, cmd.HouseholdID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1)`, cmd.ReviewItemID); err != nil {
		return result, err
	}
	if cmd.Ignore {
		_, err = tx.Exec(ctx, `UPDATE source_event SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status IN ('PENDING','REVIEW')) THEN 'NEEDS_REVIEW' WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status='APPLIED') THEN 'PROCESSED' ELSE 'IGNORED' END WHERE id=$1 AND household_id=$2`, sourceID, cmd.HouseholdID)
	} else {
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='RECEIVED' WHERE id=$1 AND household_id=$2`, sourceID, cmd.HouseholdID); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) VALUES('PROCESS_FINANCIAL_EMAIL',jsonb_build_object('source_event_id',$1::uuid,'financial_source_id',(SELECT financial_source_id FROM financial_email_event WHERE source_event_id=$1)),5)`, sourceID)
		}
	}
	return result, err
}

// ValidateFinancialFundingAccount checks a partial choice before it is persisted.
func ValidateFinancialFundingAccount(ctx context.Context, tx pgx.Tx, householdID, accountID string) error {
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account WHERE id=$1 AND household_id=$2 AND active)`, accountID, householdID).Scan(&valid); err != nil {
		return invalidEntityIDError(err, ErrAccountInvalid)
	}
	if !valid {
		return ErrAccountInvalid
	}
	return nil
}

// invalidEntityIDError keeps malformed identifier input a client error: an
// unparseable UUID reaches Postgres as a 22P02 syntax error, so callers map it
// to 400 rather than reporting bad input as a server error.
func invalidEntityIDError(err, invalid error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return invalid
	}
	return err
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
