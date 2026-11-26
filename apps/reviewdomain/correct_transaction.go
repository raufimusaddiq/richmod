package reviewdomain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const MaxCorrectionAgeDays = 400

const (
	FactsLockNotConfirmed = "NOT_CONFIRMED"
	FactsLockTransfer     = "TRANSFER"
	FactsLockType         = "NOT_EDITABLE_TYPE"
	FactsLockSalary       = "SALARY"
	FactsLockMerged       = "MERGED"
	FactsLockSource       = "SOURCE_EVIDENCE"
)

var HandEnteredSourceTypes = []string{"TELEGRAM_TEXT", "TELEGRAM_CALLBACK", "WEB_MANUAL"}

var (
	ErrCorrectionNotAllowed  = errors.New("reviewdomain: transaction cannot be corrected here")
	ErrCorrectionEmpty       = errors.New("reviewdomain: correction changes nothing")
	ErrCorrectionStale       = errors.New("reviewdomain: transaction changed since it was loaded")
	ErrCorrectionDateFuture  = errors.New("reviewdomain: correction date is in the future")
	ErrCorrectionDateTooOld  = errors.New("reviewdomain: correction date is too old")
	ErrCorrectionAmountShape = errors.New("reviewdomain: correction amount must be a positive whole IDR amount")
)

type CorrectionLockedError struct{ Reason string }

func (e CorrectionLockedError) Error() string {
	return "reviewdomain: transaction facts are locked: " + e.Reason
}

func (e CorrectionLockedError) Is(target error) bool { return target == ErrCorrectionNotAllowed }

var wholeIDR = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func HandEnteredFactsLock(ctx context.Context, q RowQuerier, householdID, transactionID string) (string, error) {
	var reason string
	err := q.QueryRow(ctx, `SELECT CASE
			WHEN t.status<>'CONFIRMED' THEN $3
			WHEN t.type='TRANSFER' OR t.related_wealth_account_id IS NOT NULL THEN $4
			WHEN t.type NOT IN ('INCOME','EXPENSE') THEN $9
			WHEN EXISTS(SELECT 1 FROM salary_event se WHERE se.transaction_id=t.id) THEN $5
			WHEN EXISTS(SELECT 1 FROM reconciliation_merge rm WHERE rm.status='ACTIVE' AND (rm.source_transaction_id=t.id OR rm.target_transaction_id=t.id)) THEN $6
			WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN source_event s ON s.id=te.source_event_id WHERE te.transaction_id=t.id AND NOT s.source_type=ANY($7::text[])) THEN $8
			ELSE '' END
		FROM transaction t WHERE t.id=$1 AND t.household_id=$2`,
		transactionID, householdID, FactsLockNotConfirmed, FactsLockTransfer, FactsLockSalary, FactsLockMerged, HandEnteredSourceTypes, FactsLockSource, FactsLockType).Scan(&reason)
	return reason, err
}

type CorrectionCommand struct {
	HouseholdID       string
	ActorUserID       string
	ActorType         string
	TransactionID     string
	ReviewRequestID   string
	PendingActionID   string
	CategoryID        string
	Description       string
	AmountIDR         string
	CounterpartyName  *string
	MerchantID        string
	TransactionAt     *time.Time
	ExpectedUpdatedAt *time.Time
	HandEnteredOnly   bool
	AllowAnyType      bool
	Now               time.Time
}

func CorrectConfirmedTransaction(ctx context.Context, tx pgx.Tx, cmd CorrectionCommand) error {
	cmd.AmountIDR = strings.TrimSpace(cmd.AmountIDR)
	cmd.Description = strings.TrimSpace(cmd.Description)
	empty := cmd.CategoryID == "" && cmd.TransactionAt == nil && cmd.AmountIDR == "" && cmd.Description == "" && cmd.CounterpartyName == nil
	if empty && !cmd.HandEnteredOnly {
		return ErrCorrectionEmpty
	}
	if cmd.AmountIDR != "" && !wholeIDR.MatchString(cmd.AmountIDR) {
		return ErrCorrectionAmountShape
	}
	var status, kind, amount, description, counterparty string
	var categoryID, merchantID *string
	var at, updatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT status,type,category_id::text,transaction_at,amount::text,COALESCE(description,''),COALESCE(counterparty_name,''),merchant_id::text,updated_at FROM transaction WHERE id=$1 AND household_id=$2 FOR UPDATE`, cmd.TransactionID, cmd.HouseholdID).
		Scan(&status, &kind, &categoryID, &at, &amount, &description, &counterparty, &merchantID, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCorrectionNotAllowed
	}
	if err != nil {
		return fmt.Errorf("lock transaction for correction: %w", err)
	}
	if cmd.ExpectedUpdatedAt != nil && !updatedAt.Equal(*cmd.ExpectedUpdatedAt) {
		return ErrCorrectionStale
	}
	otherTypeOK := cmd.AllowAnyType && (kind != "TRANSFER" || (cmd.CategoryID == "" && cmd.AmountIDR == "" && cmd.CounterpartyName == nil))
	if status != "CONFIRMED" || (kind != "EXPENSE" && kind != "INCOME" && !otherTypeOK) {
		return ErrCorrectionNotAllowed
	}
	amountChanged := cmd.AmountIDR != "" && cmd.AmountIDR != amount
	merchantChanged := cmd.CounterpartyName != nil && *cmd.CounterpartyName != counterparty
	dateChanged := cmd.TransactionAt != nil && !cmd.TransactionAt.Equal(at)
	if cmd.HandEnteredOnly || amountChanged || merchantChanged {
		reason, err := HandEnteredFactsLock(ctx, tx, cmd.HouseholdID, cmd.TransactionID)
		if err != nil {
			return fmt.Errorf("check hand-entered facts: %w", err)
		}
		if reason != "" {
			return CorrectionLockedError{Reason: reason}
		}
	}
	if empty {
		return ErrCorrectionEmpty
	}
	if dateChanged {
		now := cmd.Now
		if now.IsZero() {
			now = time.Now()
		}
		today := jakartaDay(now)
		switch day := jakartaDay(*cmd.TransactionAt); {
		case day.After(today):
			return ErrCorrectionDateFuture
		case day.Before(today.AddDate(0, 0, -MaxCorrectionAgeDays)):
			return ErrCorrectionDateTooOld
		}
	}
	if cmd.CategoryID != "" {
		if err := ValidateCategoryForHousehold(ctx, tx, cmd.HouseholdID, cmd.CategoryID); err != nil {
			return err
		}
	}
	newCategory, newAt, newAmount, newDescription, newCounterparty, newMerchant := categoryID, at, amount, description, counterparty, merchantID
	if cmd.CategoryID != "" {
		newCategory = &cmd.CategoryID
	}
	if dateChanged {
		newAt = *cmd.TransactionAt
	}
	if amountChanged {
		newAmount = cmd.AmountIDR
	}
	if cmd.Description != "" {
		newDescription = cmd.Description
	}
	if merchantChanged {
		newCounterparty = *cmd.CounterpartyName
		newMerchant = nil
		if cmd.MerchantID != "" {
			newMerchant = &cmd.MerchantID
		}
	}
	categoryChanged := cmd.CategoryID != "" && (categoryID == nil || *categoryID != cmd.CategoryID)
	if !categoryChanged && !dateChanged && !amountChanged && !merchantChanged && newDescription == description {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction SET category_id=$3::uuid,transaction_at=$4,amount=$5::numeric,description=NULLIF($6::text,''),counterparty_name=NULLIF($7::text,''),merchant_id=$8::uuid,updated_at=now() WHERE id=$1 AND household_id=$2`,
		cmd.TransactionID, cmd.HouseholdID, newCategory, newAt, newAmount, newDescription, newCounterparty, newMerchant); err != nil {
		return fmt.Errorf("correct transaction: %w", err)
	}
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json)
		VALUES($1,$2,$3,'EDIT_TRANSACTION','transaction',$4,
			jsonb_build_object('category_id',$5::text,'transaction_at',$6::timestamptz,'amount',$7::text,'description',$8::text,'counterparty_name',$9::text),
			jsonb_build_object('category_id',$10::text,'transaction_at',$11::timestamptz,'amount',$12::text,'description',$13::text,'counterparty_name',$14::text,'review_request_id',NULLIF($15,''),'pending_action_id',NULLIF($16,'')))`,
		cmd.HouseholdID, actorType, nullableActor(cmd.ActorUserID), cmd.TransactionID, categoryID, at, amount, description, counterparty,
		newCategory, newAt, newAmount, newDescription, newCounterparty, cmd.ReviewRequestID, cmd.PendingActionID); err != nil {
		return fmt.Errorf("audit transaction correction: %w", err)
	}
	if dateChanged || amountChanged {
		if err := RefreshOpenCycleResiduals(ctx, tx, cmd.HouseholdID, at, cmd.ActorUserID); err != nil {
			return err
		}
		if dateChanged {
			if err := RefreshOpenCycleResiduals(ctx, tx, cmd.HouseholdID, newAt, cmd.ActorUserID); err != nil {
				return err
			}
		}
	}
	return nil
}

func nullableActor(id string) any {
	if id == "" {
		return nil
	}
	return id
}
