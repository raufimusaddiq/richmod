package reviewdomain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ConfirmCommand struct {
	HouseholdID      string
	ActorUserID      string
	TransactionID    string
	ReviewItemID     string
	RequestID        string
	Action           string
	CategorySupplied bool
	CategoryID       string
	Description      string
	Note             string
	MerchantName     string
	RememberMerchant bool
	TransactionAt    *time.Time
	Blocked          []string
	ResolveReview    bool
}

type ConfirmResult struct {
	CategoryID string
	MerchantID string
}

var (
	ErrMissingCategory                = errors.New("reviewdomain: expense category is required")
	ErrTransferClassificationRequired = errors.New("reviewdomain: transfer classification required")
	ErrMerchantRequiredForLearning    = errors.New("reviewdomain: merchant is required to remember a category")
)

type ErrMissingFacts struct{ Facts []string }

func (e *ErrMissingFacts) Error() string {
	return "reviewdomain: unresolved residual facts: " + strings.Join(e.Facts, ",")
}

func ConfirmTransactionReview(ctx context.Context, tx pgx.Tx, cmd ConfirmCommand) (ConfirmResult, error) {
	var result ConfirmResult
	if err := ValidateTransactionReview(ctx, tx, cmd.HouseholdID, cmd.TransactionID); err != nil {
		return result, err
	}
	var disputed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction_evidence e JOIN transaction t ON t.id=e.transaction_id WHERE t.id=$1 AND t.household_id=$2 AND e.metadata_json->>'evidence_link_dispute'='true' AND COALESCE(e.metadata_json->>'link_status','ACTIVE')='ACTIVE')`, cmd.TransactionID, cmd.HouseholdID).Scan(&disputed); err != nil {
		return result, err
	}
	if disputed && cmd.Action != "CONFIRM_EVIDENCE_DISPUTE" {
		return result, ErrEvidenceDisputeConfirmationRequired
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
	if kind == "EXPENSE" && categoryID == nil {
		return result, ErrMissingCategory
	}
	if len(cmd.Blocked) > 0 {
		return result, &ErrMissingFacts{Facts: cmd.Blocked}
	}
	if kind == "UNCLASSIFIED" {
		return result, ErrTransferClassificationRequired
	}
	relationship := "DISTINCT"
	if kind == "INCOME" {
		relationship = "INCOME"
	}
	if kind == "TRANSFER" {
		relationship = ""
	}
	facts := ReviewFacts{HouseholdID: cmd.HouseholdID, TransactionID: cmd.TransactionID, TransactionAt: cmd.TransactionAt, Relationship: relationship}
	if cmd.CategorySupplied {
		facts.CategoryID = cmd.CategoryID
	}
	remaining, err := ApplyTransactionReviewFacts(ctx, tx, facts)
	if err != nil {
		return result, err
	}
	if len(remaining) > 0 {
		return result, &ErrMissingFacts{Facts: remaining}
	}
	merchantName := cleanText(cmd.MerchantName, 160)
	if merchantName != "" {
		created, err := ResolveOrCreateMerchant(ctx, tx, cmd.HouseholdID, merchantName)
		if err != nil {
			return result, err
		}
		if created == "" {
			return result, ErrAmbiguousMerchantMatch
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
	var confirmedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_at FROM transaction WHERE id=$1`, cmd.TransactionID).Scan(&confirmedAt); err != nil {
		return result, err
	}
	if err := RefreshOpenCycleResiduals(ctx, tx, cmd.HouseholdID, confirmedAt, cmd.ActorUserID); err != nil {
		return result, err
	}
	if merchantName != "" {
		if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET merchant_raw=$2,updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')", cmd.TransactionID, merchantName); err != nil {
			return result, err
		}
	}
	if cmd.CategorySupplied {
		if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET proposal_status='ACCEPTED',category_candidate_id=$2,updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')", cmd.TransactionID, categoryID); err != nil {
			return result, err
		}
	} else {
		if _, err := tx.Exec(ctx, "UPDATE transaction_proposal SET proposal_status='ACCEPTED',updated_at=now() WHERE id IN (SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND metadata_json ? 'proposal_id' AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')", cmd.TransactionID); err != nil {
			return result, err
		}
	}
	if merchantID != nil {
		result.MerchantID = *merchantID
	}
	if categoryID != nil {
		result.CategoryID = *categoryID
	}
	if !cmd.ResolveReview {
		return result, RefreshTransactionLifecycle(ctx, tx, cmd.HouseholdID, cmd.TransactionID)
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

func cleanText(value string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
