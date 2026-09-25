// Package reviewdomain holds the canonical, channel-neutral review resolution
// operations for Richmod (ADR-046). Web and Telegram adapters call these
// functions inside their own transaction; no surface owns financial mutation
// policy for a review transition.
package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrAlreadyResolved reports a resolution attempt against a review that is no
// longer open. Callers surface a deterministic stale/resolved response.
var ErrAlreadyResolved = errors.New("reviewdomain: review already resolved")

// resolveSQL records the human decision and leaves projection resolution to the
// caller-supplied transaction, so Web and Telegram share one completion path.
const resolveSQL = `UPDATE review_item
		SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,
		    resolution_values=$4::jsonb,updated_at=now()
		WHERE id=$1 AND status IN ('PENDING_SEND','OPEN')`

const resolveRequestSQL = `UPDATE review_request SET status='RESOLVED',resolved_at=now()
		WHERE (review_item_id=$1 OR transaction_id=$2) AND status IN ('PENDING_SEND','OPEN')`

const resolveByIDSQL = `UPDATE review_item
		SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,
		    resolution_values=$4::jsonb,updated_at=now()
		WHERE id=$1 AND household_id=$5 AND status IN ('PENDING_SEND','OPEN')`

// Command is a channel-neutral resolution request. Surface, callback, and reply
// binding stay in the adapters; this type carries only canonical inputs.
type Command struct {
	HouseholdID  string
	ActorUserID  string
	ReviewItemID string
	// SubjectID is the review subject identity (a transaction ID for the
	// transaction operation family).
	SubjectID string
	Action    string
	// Values carries the canonical residual values recorded with the resolution.
	Values []byte
}

// ResolveByTransaction completes every open review_item bound to one transaction
// and household. It takes the canonical lock, verifies the review is still
// active, records the human actor/action/values, and resolves the Telegram
// projection in the same transaction.
//
// Financial fact validation and the review-family mutation stay with the caller
// until each family is migrated; this function owns the terminal review
// transition, which previously existed twice (API and Telegram).
func ResolveByTransaction(ctx context.Context, tx pgx.Tx, cmd Command) error {
	values := cmd.Values
	if len(values) == 0 {
		var err error
		values, err = json.Marshal(map[string]string{"transaction_id": cmd.SubjectID})
		if err != nil {
			return err
		}
	}
	var reviewItemID string
	err := tx.QueryRow(ctx, `SELECT id FROM review_item
		WHERE household_id=$1 AND transaction_id=$2 AND status IN ('PENDING_SEND','OPEN')
		FOR UPDATE`, cmd.HouseholdID, cmd.SubjectID).Scan(&reviewItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, resolveSQL, reviewItemID, cmd.ActorUserID, cmd.Action, string(values))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	_, err = tx.Exec(ctx, resolveRequestSQL, reviewItemID, cmd.SubjectID)
	return err
}

// ResolveByID completes one exact review item after locking and validating its
// household, subject, and active status. Telegram uses this to preserve stored
// message/request binding; the caller must authenticate actorUserID first.
func ResolveByID(ctx context.Context, tx pgx.Tx, cmd Command) error {
	values := cmd.Values
	if len(values) == 0 {
		var err error
		values, err = json.Marshal(map[string]string{"transaction_id": cmd.SubjectID})
		if err != nil {
			return err
		}
	}
	var subjectID string
	err := tx.QueryRow(ctx, `SELECT COALESCE(transaction_id::text,'') FROM review_item
		WHERE id=$1 AND household_id=$2 AND status IN ('PENDING_SEND','OPEN') FOR UPDATE`, cmd.ReviewItemID, cmd.HouseholdID).Scan(&subjectID)
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
	result, err := tx.Exec(ctx, resolveByIDSQL, cmd.ReviewItemID, cmd.ActorUserID, cmd.Action, string(values), cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	_, err = tx.Exec(ctx, resolveRequestSQL, cmd.ReviewItemID, subjectID)
	return err
}
