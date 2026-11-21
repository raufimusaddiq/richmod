package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type BankFactCommand struct {
	HouseholdID   string
	UserID        string
	ReviewItemID  string
	SourceEventID string
	AccountID     string
	AmountIDR     string
	TransactionAt string
}

type BankAccountChoice struct {
	ID   string
	Name string
}

var (
	ErrBankFactsRequired     = errors.New("reviewdomain: bank amount and timestamp are required")
	ErrBankReviewUnavailable = errors.New("reviewdomain: bank review is unavailable")
	ErrBankSourceUnlinked    = errors.New("reviewdomain: bank source has no active account")
	ErrBankAccountInvalid    = errors.New("reviewdomain: selected bank account is not available")
)

func IgnoreBankReview(ctx context.Context, tx pgx.Tx, cmd BankFactCommand, actorType string) error {
	var sourceID string
	var decision []byte
	err := tx.QueryRow(ctx, `SELECT s.id::text,ri.decision FROM review_item ri
		JOIN source_event s ON s.id=ri.source_event_id AND s.household_id=ri.household_id
		WHERE ri.id=$1 AND ri.household_id=$2 AND ri.review_type='UNKNOWN_BANK_TEMPLATE'
		AND ri.transaction_id IS NULL AND s.source_type='BANK_EMAIL'
		AND ri.status IN ('OPEN','PENDING_SEND') FOR UPDATE OF ri,s`, cmd.ReviewItemID, cmd.HouseholdID).Scan(&sourceID, &decision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBankReviewUnavailable
	}
	if err != nil {
		return err
	}
	var contract struct {
		AllowedActions []string `json:"allowedActions"`
	}
	if json.Unmarshal(decision, &contract) != nil || !containsAction(contract.AllowedActions, "IGNORE") {
		return ErrBankReviewUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='IGNORE',resolution_values='{}'::jsonb,updated_at=now() WHERE id=$1 AND household_id=$3`, cmd.ReviewItemID, cmd.UserID, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, resolveRequestByItemSQL, []string{cmd.ReviewItemID}, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1 AND household_id=$2)`, cmd.ReviewItemID, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action','IGNORE'))`, cmd.HouseholdID, actorType, cmd.UserID, cmd.ReviewItemID); err != nil {
		return err
	}
	return MarkSourceIgnored(ctx, tx, cmd.HouseholdID, sourceID)
}

func ListBankSourceAccountChoices(ctx context.Context, tx pgx.Tx, householdID, reviewItemID, sourceEventID string, limit int) ([]BankAccountChoice, error) {
	var unlinked bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(l.account_id::text,'')='' FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id JOIN bank_email_extraction e ON e.source_event_id=s.id JOIN bank_email_listener l ON l.id=e.listener_id WHERE ri.id=$1 AND ri.household_id=$2 AND ri.source_event_id=$3 AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.status IN ('OPEN','PENDING_SEND')`, reviewItemID, householdID, sourceEventID).Scan(&unlinked); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBankReviewUnavailable
	} else if err != nil {
		return nil, err
	} else if !unlinked {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text,name FROM account WHERE household_id=$1 AND active ORDER BY name,id LIMIT $2`, householdID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var choices []BankAccountChoice
	for rows.Next() {
		var c BankAccountChoice
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		choices = append(choices, c)
	}
	return choices, rows.Err()
}

func BindBankSourceAccount(ctx context.Context, tx pgx.Tx, householdID, reviewItemID, sourceEventID, accountID string) error {
	if err := ValidateFinancialFundingAccount(ctx, tx, householdID, accountID); err != nil {
		if errors.Is(err, ErrAccountInvalid) {
			return ErrBankAccountInvalid
		}
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE bank_email_listener l SET account_id=$4,updated_at=now() FROM bank_email_extraction e, review_item ri WHERE e.source_event_id=$3 AND l.id=e.listener_id AND ri.id=$2 AND ri.household_id=$1 AND ri.source_event_id=$3 AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.status IN ('OPEN','PENDING_SEND') AND l.household_id=$1 AND l.account_id IS NULL`, householdID, reviewItemID, sourceEventID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrBankReviewUnavailable
	}
	return nil
}

func ValidateBankSourceAccount(ctx context.Context, tx pgx.Tx, cmd BankFactCommand) error {
	if err := ValidateBankFactValues(cmd.AmountIDR, cmd.TransactionAt); err != nil {
		return err
	}
	var listenerAccount string
	err := tx.QueryRow(ctx, `SELECT COALESCE(l.account_id::text,'') FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id JOIN bank_email_extraction e ON e.source_event_id=s.id JOIN bank_email_listener l ON l.id=e.listener_id WHERE ri.id=$1 AND ri.household_id=$2 AND ri.source_event_id=$3 AND ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.status IN ('OPEN','PENDING_SEND') FOR UPDATE OF ri`, cmd.ReviewItemID, cmd.HouseholdID, cmd.SourceEventID).Scan(&listenerAccount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBankReviewUnavailable
	}
	if err != nil {
		return err
	}
	if listenerAccount == "" {
		return ErrBankSourceUnlinked
	}
	if err := ValidateFinancialFundingAccount(ctx, tx, cmd.HouseholdID, listenerAccount); err != nil {
		return err
	}
	return nil
}

func ValidateBankFactValues(amount, timestamp string) error {
	if strings.TrimSpace(amount) == "" || strings.TrimSpace(timestamp) == "" {
		return ErrBankFactsRequired
	}
	if !ValidBankAmountIDR(amount) {
		return ErrBankFactsRequired
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(timestamp)); err != nil {
		return ErrBankFactsRequired
	}
	return nil
}

func ValidBankAmountIDR(amount string) bool {
	return len(amount) > 0 && len(amount) <= 20 && strings.Trim(amount, "0123456789") == "" && strings.Trim(amount, "0") != ""
}
