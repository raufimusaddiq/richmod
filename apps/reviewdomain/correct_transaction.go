package reviewdomain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrCorrectionNotAllowed = errors.New("reviewdomain: transaction cannot be corrected here")
	ErrCorrectionEmpty      = errors.New("reviewdomain: correction changes nothing")
)

type CorrectionCommand struct {
	HouseholdID     string
	ActorUserID     string
	ActorType       string
	TransactionID   string
	ReviewRequestID string
	CategoryID      string
	TransactionAt   *time.Time
}

func CorrectConfirmedTransaction(ctx context.Context, tx pgx.Tx, cmd CorrectionCommand) error {
	if cmd.CategoryID == "" && cmd.TransactionAt == nil {
		return ErrCorrectionEmpty
	}
	var status, kind string
	var categoryID *string
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT status,type,category_id::text,transaction_at FROM transaction WHERE id=$1 AND household_id=$2 FOR UPDATE`, cmd.TransactionID, cmd.HouseholdID).Scan(&status, &kind, &categoryID, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCorrectionNotAllowed
	}
	if err != nil {
		return fmt.Errorf("lock transaction for correction: %w", err)
	}
	if status != "CONFIRMED" || (kind != "EXPENSE" && kind != "INCOME") {
		return ErrCorrectionNotAllowed
	}
	if cmd.CategoryID != "" {
		if err := ValidateCategoryForHousehold(ctx, tx, cmd.HouseholdID, cmd.CategoryID); err != nil {
			return err
		}
	}
	newCategory, newAt := categoryID, at
	if cmd.CategoryID != "" {
		newCategory = &cmd.CategoryID
	}
	if cmd.TransactionAt != nil {
		newAt = *cmd.TransactionAt
	}
	categoryChanged := cmd.CategoryID != "" && (categoryID == nil || *categoryID != cmd.CategoryID)
	dateChanged := !newAt.Equal(at)
	if !categoryChanged && !dateChanged {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE transaction SET category_id=$3::uuid,transaction_at=$4,updated_at=now() WHERE id=$1 AND household_id=$2`, cmd.TransactionID, cmd.HouseholdID, newCategory, newAt); err != nil {
		return fmt.Errorf("correct transaction: %w", err)
	}
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,before_json,after_json)
		VALUES($1,$2,$3,'EDIT_TRANSACTION','transaction',$4,
			jsonb_build_object('category_id',$5::text,'transaction_at',$6::timestamptz),
			jsonb_build_object('category_id',$7::text,'transaction_at',$8::timestamptz,'review_request_id',NULLIF($9,'')))`,
		cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.TransactionID, categoryID, at, newCategory, newAt, cmd.ReviewRequestID); err != nil {
		return fmt.Errorf("audit transaction correction: %w", err)
	}
	if dateChanged {
		if err := RefreshOpenCycleResiduals(ctx, tx, cmd.HouseholdID, at, cmd.ActorUserID); err != nil {
			return err
		}
		if err := RefreshOpenCycleResiduals(ctx, tx, cmd.HouseholdID, newAt, cmd.ActorUserID); err != nil {
			return err
		}
	}
	return nil
}
