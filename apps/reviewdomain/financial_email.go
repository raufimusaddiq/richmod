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

type FinancialEmailCommand struct {
	HouseholdID     string
	ObservationID   string
	ReviewItemID    string
	Ignore          bool
	AllowPartial    bool
	AccountID       string
	WealthAccountID string
	AccountHint     string
	ProviderHint    string
	SourceEventID   string
	ActorUserID     string
	ActorType       string
}

type FinancialEmailResult struct {
	Complete        bool
	ObservationID   string
	AccountID       string
	WealthAccountID string
	HumanSupplied   []string
	Values          []byte
}

var (
	ErrFinancialObservationUnavailable = errors.New("reviewdomain: financial observation is unavailable")
	ErrFundingAccountRequired          = errors.New("reviewdomain: the unresolved funding account is required")
	ErrProviderAccountRequired         = errors.New("reviewdomain: the unresolved Wealth Account is required")
	ErrAccountInvalid                  = errors.New("reviewdomain: account must be active and belong to this household")
)

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
	if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET resolved_account_id=NULLIF($2,'')::uuid,resolved_wealth_account_id=NULLIF($3,'')::uuid,status=CASE WHEN $4::boolean THEN 'PENDING' ELSE 'REVIEW' END,updated_at=now() WHERE id=$1 AND household_id=$5`, observationID, accountID, wealthAccountID, result.Complete, cmd.HouseholdID); err != nil {
		return result, err
	}
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

func ResolveFinancialEmailReview(ctx context.Context, tx pgx.Tx, cmd FinancialEmailCommand) (FinancialEmailResult, error) {
	var result FinancialEmailResult
	var sourceID string
	err := tx.QueryRow(ctx, `SELECT fo.source_event_id::text FROM review_item ri JOIN financial_email_observation fo ON fo.id=ri.financial_email_observation_id AND fo.household_id=ri.household_id WHERE ri.id=$1 AND ri.household_id=$2 AND ri.status IN ('OPEN','PENDING_SEND') AND (ri.review_type='FINANCIAL_EMAIL_RESOLUTION' OR ($4::boolean AND ri.review_type='TRANSFER_CLASSIFICATION')) AND fo.id=$3 AND fo.status='REVIEW' FOR UPDATE OF ri,fo`, cmd.ReviewItemID, cmd.HouseholdID, cmd.ObservationID, cmd.Ignore).Scan(&sourceID)
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
		if err != nil {
			return result, err
		}
		if !result.Complete {
			_, err = tx.Exec(ctx, `UPDATE review_item SET decision=jsonb_set(COALESCE(decision,'{}'::jsonb),'{missingFacts}',CASE WHEN $2::text='' THEN '["funding_account"]'::jsonb ELSE '["wealth_account"]'::jsonb END),updated_at=now() WHERE id=$1 AND household_id=$3`, cmd.ReviewItemID, result.AccountID, cmd.HouseholdID)
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
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action',$5::text))`, cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.ReviewItemID, action); err != nil {
		return result, err
	}
	if cmd.Ignore {
		if _, err = tx.Exec(ctx, `UPDATE transfer_reconciliation_case SET status='RESOLVED',updated_at=now() WHERE financial_email_observation_id=$1 AND household_id=$2 AND status IN ('OPEN','PENDING_SEND')`, cmd.ObservationID, cmd.HouseholdID); err == nil {
			if _, err = tx.Exec(ctx, `UPDATE source_event SET parser_name='financial-email-reconciliation',parser_version='1' WHERE id=$1 AND household_id=$2`, sourceID, cmd.HouseholdID); err == nil {
				err = MarkSourceIgnored(ctx, tx, cmd.HouseholdID, sourceID)
			}
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='RECEIVED' WHERE id=$1 AND household_id=$2`, sourceID, cmd.HouseholdID); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO job(type,payload_json,max_attempts) VALUES('PROCESS_FINANCIAL_EMAIL',jsonb_build_object('source_event_id',$1::uuid,'financial_source_id',(SELECT financial_source_id FROM financial_email_event WHERE source_event_id=$1)),5)`, sourceID)
		}
	}
	return result, err
}

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

func invalidEntityIDError(err, invalid error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return invalid
	}
	return err
}

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

func LearnEntityAliasIfNew(ctx context.Context, tx pgx.Tx, householdID, entityType, entityID, alias, known string) error {
	if strings.TrimSpace(entityID) == "" {
		return nil
	}
	if strings.TrimSpace(known) != "" {
		return nil
	}
	return LearnEntityAlias(ctx, tx, householdID, entityType, entityID, alias)
}

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
