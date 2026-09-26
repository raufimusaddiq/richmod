package reviewdomain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ConfirmCommand carries the bounded residual facts one confirm collects. Zero
// values mean "not supplied this turn" and never overwrite stored state.
type ConfirmCommand struct {
	HouseholdID   string
	ActorUserID   string
	TransactionID string
	// ReviewItemID pins the exact canonical item. Empty completes every open item
	// for the transaction, which is the Web behavior.
	ReviewItemID string
	// RequestID is the exact Telegram projection identity; never substitute the
	// review_item ID here.
	RequestID string
	// ReviewType scopes bounded duplicate resolution without changing the normal
	// confirmation behavior for other transaction reviews.
	ReviewType string
	Action     string
	// CategorySupplied distinguishes an explicit choice from "unchanged".
	CategorySupplied bool
	CategoryID       string
	Description      string
	Note             string
	MerchantName     string
	RememberMerchant bool
	// TransactionAt is a column-compatible timestamp argument, nil when absent.
	// It is `any` because surfaces pass a *string, a time.Time, or a *time.Time;
	// ConfirmTransactionReview normalizes a typed-nil pointer before use.
	TransactionAt any
	// Blocked lists stored residual facts the caller did not supply this turn.
	Blocked []string
	// ResolveReview selects the terminal completion. Telegram keeps the review open
	// while it asks whether to remember the merchant, then completes on the answer.
	ResolveReview bool
}

// ConfirmResult reports what the canonical confirm recorded so each surface can
// map it to its own response shape.
type ConfirmResult struct {
	CategoryID string
	MerchantID string
}

var (
	// ErrMissingCategory reports an expense confirm that supplied no category.
	ErrMissingCategory = errors.New("reviewdomain: expense category is required")
	// ErrTransferClassificationRequired reports a confirm on an unclassified
	// transfer, which must use transfer classification instead.
	ErrTransferClassificationRequired = errors.New("reviewdomain: transfer classification required")
	// ErrMerchantRequiredForLearning reports a remember request with no merchant.
	ErrMerchantRequiredForLearning = errors.New("reviewdomain: merchant is required to remember a category")
)

// ErrMissingFacts reports a confirm whose stored decision still requires
// residual facts the caller did not supply this turn.
type ErrMissingFacts struct{ Facts []string }

func (e *ErrMissingFacts) Error() string {
	return "reviewdomain: unresolved residual facts: " + strings.Join(e.Facts, ",")
}

// ConfirmTransactionReview performs the canonical transaction-review confirm: it
// locks the subject, revalidates candidates, mutates the transaction and its
// proposal, keeps source-event status current, and optionally completes the
// review item(s) plus Telegram projection. Web and Telegram both call this, so
// neither surface owns transaction confirm policy (ADR-046).
func ConfirmTransactionReview(ctx context.Context, tx pgx.Tx, cmd ConfirmCommand) (ConfirmResult, error) {
	var result ConfirmResult
	// A typed-nil *time.Time stored in the `any` field is non-nil as an interface:
	// surfaces that pass an absent date as a nil pointer would otherwise pass the
	// `!= nil` guard below and write NULL into the NOT NULL
	// transaction_proposal.transaction_at. Normalize it away at the boundary.
	// Telegram passes the date as a *string (the canonical `YYYY-MM-DD` it just
	// parsed), so every pointer type must be normalized, not just *time.Time.
	switch at := cmd.TransactionAt.(type) {
	case *time.Time:
		if at == nil {
			cmd.TransactionAt = nil
		}
	case *string:
		if at == nil {
			cmd.TransactionAt = nil
		}
	}
	if err := ValidateTransactionReview(ctx, tx, cmd.HouseholdID, cmd.TransactionID); err != nil {
		return result, err
	}
	var kind string
	var currentCategory, merchantID *string
	err := tx.QueryRow(ctx, "SELECT type,category_id,merchant_id FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE", cmd.TransactionID, cmd.HouseholdID).Scan(&kind, &currentCategory, &merchantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrAlreadyResolved
	}
	if err != nil {
		return result, err
	}
	categoryID := currentCategory
	if cmd.CategorySupplied {
		if err := ValidateCategoryForHousehold(ctx, tx, cmd.HouseholdID, cmd.CategoryID); err != nil {
			return result, err
		}
		if cmd.CategoryID == "" {
			categoryID = nil
		} else {
			selected := cmd.CategoryID
			categoryID = &selected
		}
	}
	if kind == "EXPENSE" && categoryID == nil && cmd.ReviewType != "POSSIBLE_DUPLICATE" {
		return result, ErrMissingCategory
	}
	if len(cmd.Blocked) > 0 {
		return result, &ErrMissingFacts{Facts: cmd.Blocked}
	}
	if kind == "UNCLASSIFIED" {
		return result, ErrTransferClassificationRequired
	}
	merchantName := cleanText(cmd.MerchantName, 160)
	if merchantName != "" {
		var created string
		if err := tx.QueryRow(ctx, "INSERT INTO merchant(household_id,normalized_name) VALUES($1,regexp_replace(trim($2), '[[:space:]]+', ' ', 'g')) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET updated_at=now() RETURNING id", cmd.HouseholdID, merchantName).Scan(&created); err != nil {
			return result, err
		}
		merchantID = &created
	}
	if cmd.RememberMerchant && merchantID == nil {
		return result, ErrMerchantRequiredForLearning
	}
	_, err = tx.Exec(ctx, "UPDATE transaction SET status='CONFIRMED',category_id=$2,merchant_id=COALESCE($5::uuid,merchant_id),description=COALESCE(NULLIF($3,''),description),note=COALESCE(NULLIF($4,''),note),transaction_at=COALESCE($6,transaction_at),confirmed_at=now(),voided_at=NULL,updated_at=now() WHERE id=$1 AND status='NEEDS_REVIEW'", cmd.TransactionID, categoryID, cleanText(cmd.Description, 500), cleanText(cmd.Note, 1000), merchantID, cmd.TransactionAt)
	if err != nil {
		return result, err
	}
	if cmd.TransactionAt != nil {
		if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET transaction_at=$2,metadata_json=metadata_json||'{\"date_known\":true,\"date_source\":\"USER\"}'::jsonb,updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')", cmd.TransactionID, cmd.TransactionAt); err != nil {
			return result, err
		}
	}
	if merchantName != "" {
		if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET merchant_raw=$2,updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')", cmd.TransactionID, merchantName); err != nil {
			return result, err
		}
	}
	// Only overwrite the stored candidate when this turn supplied a category;
	// otherwise an unmentioned candidate survives the confirm, which is what both
	// surfaces did before the operation was shared.
	if cmd.CategorySupplied {
		if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET proposal_status='ACCEPTED',category_candidate_id=$2,updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')", cmd.TransactionID, categoryID); err != nil {
			return result, err
		}
	} else {
		if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET proposal_status='ACCEPTED',updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id')", cmd.TransactionID); err != nil {
			return result, err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE source_event s SET processing_status=CASE WHEN EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction other_t ON other_t.id=te.transaction_id WHERE te.source_event_id=s.id AND other_t.status='NEEDS_REVIEW') THEN 'NEEDS_REVIEW' ELSE 'PROCESSED' END WHERE s.id IN (SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)", cmd.TransactionID); err != nil {
		return result, err
	}
	if merchantID != nil {
		result.MerchantID = *merchantID
	}
	if categoryID != nil {
		result.CategoryID = *categoryID
	}
	if !cmd.ResolveReview {
		return result, nil
	}
	action := cmd.Action
	if action == "" {
		action = "CONFIRM_REVIEW"
	}
	if cmd.ReviewItemID == "" {
		err = ResolveByTransaction(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, SubjectID: cmd.TransactionID, Action: action})
	} else {
		err = ResolveByID(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, ReviewItemID: cmd.ReviewItemID, RequestID: cmd.RequestID, SubjectID: cmd.TransactionID, Action: action})
	}
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "UPDATE review_conversation SET state='RESOLVED',updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE transaction_id=$1 AND status='RESOLVED')", cmd.TransactionID); err != nil {
		return result, err
	}
	if cmd.RememberMerchant && merchantID != nil && categoryID != nil {
		if _, err := tx.Exec(ctx, "INSERT INTO merchant_alias (household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,normalized_name,id,$3,true,true FROM merchant WHERE id=$2 ON CONFLICT (household_id,lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g'))) DO UPDATE SET raw_name=excluded.raw_name,default_category_id=excluded.default_category_id,auto_apply=true,created_from_user_confirmation=true", cmd.HouseholdID, merchantID, categoryID); err != nil {
			return result, err
		}
	}
	return result, nil
}

// cleanText trims and truncates surface input to its stored ceiling.
func cleanText(value string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
