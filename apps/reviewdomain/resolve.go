package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrAlreadyResolved = errors.New("reviewdomain: review already resolved")

const resolveRequestSQL = `UPDATE review_request SET status='RESOLVED',resolved_at=now()
		WHERE household_id=$3 AND (review_item_id=ANY($1::uuid[]) OR transaction_id=$2) AND status IN ('PENDING_SEND','OPEN')`

const resolveRequestByItemSQL = `UPDATE review_request SET status='RESOLVED',resolved_at=now()
		WHERE household_id=$2 AND review_item_id=ANY($1::uuid[]) AND status IN ('PENDING_SEND','OPEN')`

const resolveTransactionSQL = `UPDATE review_item
		SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$3,resolution_action=$4,
		    resolution_values=$5::jsonb,updated_at=now()
		WHERE household_id=$1 AND transaction_id=$2 AND status IN ('PENDING_SEND','OPEN')
		RETURNING id`

const resolveByIDSQL = `UPDATE review_item
		SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,
		    resolution_values=$4::jsonb,updated_at=now()
		WHERE id=$1 AND household_id=$5 AND status IN ('PENDING_SEND','OPEN')`

type Command struct {
	HouseholdID  string
	ActorUserID  string
	ReviewItemID string
	RequestID    string
	SubjectID    string
	Action       string
	Values       []byte
}

func ResolveByTransaction(ctx context.Context, tx pgx.Tx, cmd Command) error {
	values := cmd.Values
	if len(values) == 0 {
		var err error
		values, err = json.Marshal(map[string]string{"transaction_id": cmd.SubjectID})
		if err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, resolveTransactionSQL, cmd.HouseholdID, cmd.SubjectID, cmd.ActorUserID, cmd.Action, string(values))
	if err != nil {
		return err
	}
	var reviewItemIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		reviewItemIDs = append(reviewItemIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if _, err = tx.Exec(ctx, resolveRequestSQL, reviewItemIDs, cmd.SubjectID, cmd.HouseholdID); err != nil {
		return err
	}
	return RefreshTransactionLifecycle(ctx, tx, cmd.HouseholdID, cmd.SubjectID)
}

func ResolveByID(ctx context.Context, tx pgx.Tx, cmd Command) error {
	values := cmd.Values
	if len(values) == 0 {
		var err error
		values, err = json.Marshal(map[string]string{"transaction_id": cmd.SubjectID})
		if err != nil {
			return err
		}
	}
	itemID := cmd.ReviewItemID
	if itemID == "" {
		if err := tx.QueryRow(ctx, `SELECT review_item_id::text FROM review_request
			WHERE id=$1 AND household_id=$2`, cmd.RequestID, cmd.HouseholdID).Scan(&itemID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrAlreadyResolved
			}
			return err
		}
	}
	var subjectID string
	err := tx.QueryRow(ctx, `SELECT COALESCE(transaction_id::text,'') FROM review_item
		WHERE id=$1 AND household_id=$2 AND status IN ('PENDING_SEND','OPEN') FOR UPDATE`, itemID, cmd.HouseholdID).Scan(&subjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	if err != nil {
		return err
	}
	if cmd.SubjectID != "" && subjectID != cmd.SubjectID {
		return errors.New("reviewdomain: subject binding mismatch")
	}
	if subjectID == "" {
		return errors.New("reviewdomain: transaction subject required")
	}
	result, err := tx.Exec(ctx, resolveByIDSQL, itemID, cmd.ActorUserID, cmd.Action, string(values), cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	if _, err = tx.Exec(ctx, resolveRequestByItemSQL, []string{itemID}, cmd.HouseholdID); err != nil {
		return err
	}
	return RefreshTransactionLifecycle(ctx, tx, cmd.HouseholdID, subjectID)
}
