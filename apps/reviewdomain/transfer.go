package reviewdomain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TransferCommand struct {
	HouseholdID     string
	ActorUserID     string
	TransactionID   string
	ReviewItemID    string
	RequestID       string
	Action          string
	Classification  string
	CategoryID      string
	WealthAccountID string
	TransactionAt   *time.Time
}

type TransferResult struct {
	Type            string
	Status          string
	Purpose         string
	WealthAccountID string
	CategoryID      string
	Counterparty    *string
}

var (
	ErrTransferNotFound           = errors.New("reviewdomain: transfer review not found")
	ErrExpenseCategoryRequired    = errors.New("reviewdomain: expense category is required")
	ErrWealthAccountRequired      = errors.New("reviewdomain: asset purchase requires a Wealth Account")
	ErrInvestmentAccountAmbiguous = errors.New("reviewdomain: investment account requires a deterministic linked wealth account")
	ErrWealthAccountIncompatible  = errors.New("reviewdomain: incompatible wealth account")
)

func ClassifyTransferReview(ctx context.Context, tx pgx.Tx, cmd TransferCommand) (TransferResult, error) {
	var result TransferResult
	var transactionType, purpose string
	var counterparty *string
	err := tx.QueryRow(ctx, "SELECT type,purpose,counterparty_name FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE", cmd.TransactionID, cmd.HouseholdID).Scan(&transactionType, &purpose, &counterparty)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrTransferNotFound
	}
	if err != nil {
		return result, err
	}
	replacingAccount := transactionType == "TRANSFER" && (purpose == "ASSET_PURCHASE" && cmd.Classification == "ASSET_PURCHASE" || purpose == "INVESTMENT_CONTRIBUTION" && cmd.Classification == "INVESTMENT_ACCOUNT")
	if !replacingAccount && !transferClassificationApplies(transactionType, cmd.Classification) {
		return result, ErrTransferNotFound
	}
	result.Type, result.Status, result.Purpose = "TRANSFER", "CONFIRMED", "INTERNAL_TRANSFER"
	result.Counterparty = counterparty
	proposalStatus := "ACCEPTED"
	var wealthAccountID, categoryID *string
	if cmd.Classification == "INVESTMENT_ACCOUNT" {
		var linked string
		if cmd.WealthAccountID != "" {
			err = tx.QueryRow(ctx, "SELECT id::text FROM wealth_account WHERE id::text=$1 AND household_id=$2 AND transfer_wealth_compatible('INVESTMENT_CONTRIBUTION',id,$2::uuid)", cmd.WealthAccountID, cmd.HouseholdID).Scan(&linked)
			if errors.Is(err, pgx.ErrNoRows) {
				return result, ErrWealthAccountIncompatible
			}
			if err != nil {
				return result, err
			}
		} else {
			hint := ""
			if counterparty != nil {
				hint = strings.TrimSpace(*counterparty)
			}
			var count int
			if err = tx.QueryRow(ctx, "SELECT count(DISTINCT ka.wealth_account_id),COALESCE(min(ka.wealth_account_id::text),'') FROM known_account ka JOIN wealth_account wa ON wa.id=ka.wealth_account_id AND wa.household_id=ka.household_id AND wa.active AND wa.side='ASSET' AND wa.usage_role='INVESTMENT' WHERE ka.household_id=$1 AND ka.active AND ka.relationship='INVESTMENT_ACCOUNT' AND ka.wealth_account_id IS NOT NULL AND lower($2) LIKE '%'||lower(ka.match_hint)", cmd.HouseholdID, hint).Scan(&count, &linked); err != nil {
				return result, err
			}
			if count != 1 {
				return result, ErrInvestmentAccountAmbiguous
			}
		}
		wealthAccountID = &linked
		result.Purpose = "INVESTMENT_CONTRIBUTION"
	}
	if cmd.Classification == "ASSET_PURCHASE" {
		if strings.TrimSpace(cmd.WealthAccountID) == "" {
			return result, ErrWealthAccountRequired
		}
		var linked string
		var compatible bool
		if err = tx.QueryRow(ctx, "SELECT id::text,transfer_wealth_compatible('ASSET_PURCHASE',id,$2) FROM wealth_account WHERE id=$1 AND household_id=$2 AND active", cmd.WealthAccountID, cmd.HouseholdID).Scan(&linked, &compatible); err != nil {
			return result, ErrWealthAccountIncompatible
		}
		if !compatible {
			return result, ErrWealthAccountIncompatible
		}
		wealthAccountID = &linked
		result.Purpose = "ASSET_PURCHASE"
	}
	if cmd.Classification == "EXPENSE" {
		result.Type, result.Purpose = "EXPENSE", "GENERAL"
		if strings.TrimSpace(cmd.CategoryID) == "" {
			return result, ErrExpenseCategoryRequired
		}
		if err = ValidateCategoryForHousehold(ctx, tx, cmd.HouseholdID, cmd.CategoryID); err != nil {
			return result, err
		}
		selected := cmd.CategoryID
		categoryID = &selected
	}
	if cmd.Classification == "IGNORE" {
		result.Type, result.Status, result.Purpose = "UNCLASSIFIED", "VOIDED", "GENERAL"
		proposalStatus = "REJECTED"
	}
	if wealthAccountID != nil {
		result.WealthAccountID = *wealthAccountID
	}
	if categoryID != nil {
		result.CategoryID = *categoryID
	}
	var remaining []string
	if cmd.Classification != "IGNORE" {
		remaining, err = ApplyTransactionReviewFacts(ctx, tx, ReviewFacts{HouseholdID: cmd.HouseholdID, TransactionID: cmd.TransactionID, CategoryID: cmd.CategoryID, TransactionAt: cmd.TransactionAt, Relationship: cmd.Classification})
		if err != nil {
			return result, err
		}
		if len(remaining) > 0 {
			result.Status, proposalStatus = "NEEDS_REVIEW", "NEEDS_REVIEW"
		}
	}
	if result.Status == "VOIDED" {
		if protected, perr := ActiveMergeTarget(ctx, tx, cmd.HouseholdID, cmd.TransactionID); perr != nil {
			return result, perr
		} else if protected {
			return result, ErrMergeTargetActive
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE transaction SET type=$2,purpose=$3,related_wealth_account_id=$4,status=$5,category_id=$6,confirmed_at=CASE WHEN $5='CONFIRMED' THEN now() END,voided_at=CASE WHEN $5='VOIDED' THEN now() END,updated_at=now() WHERE id=$1 AND status='NEEDS_REVIEW'", cmd.TransactionID, result.Type, result.Purpose, wealthAccountID, result.Status, categoryID); err != nil {
		return result, MergeTargetError(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE transaction_proposal SET proposed_type=$2,proposal_status=$3,category_candidate_id=$4,metadata_json=metadata_json||jsonb_build_object('transfer_classification',$5::text,'purpose',$6::text,'related_wealth_account_id',NULLIF($7,'')::text),updated_at=now() WHERE id IN(SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1 AND COALESCE(metadata_json->>'link_status','ACTIVE')='ACTIVE')", cmd.TransactionID, result.Type, proposalStatus, categoryID, cmd.Classification, result.Purpose, result.WealthAccountID); err != nil {
		return result, err
	}
	if len(remaining) > 0 {
		return result, &ErrMissingFacts{Facts: remaining}
	}
	if _, err = tx.Exec(ctx, "UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE transaction_id=$1 AND status IN('PENDING_SEND','OPEN')", cmd.TransactionID); err != nil {
		return result, err
	}
	action := cmd.Action
	if action == "" {
		action = "CLASSIFY_TRANSFER"
	}
	if cmd.ReviewItemID == "" {
		if err = ResolveByTransaction(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, SubjectID: cmd.TransactionID, Action: action}); err != nil {
			return result, err
		}
	} else if err = ResolveByID(ctx, tx, Command{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, ReviewItemID: cmd.ReviewItemID, RequestID: cmd.RequestID, SubjectID: cmd.TransactionID, Action: action}); err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, "UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id IN(SELECT id FROM review_request WHERE transaction_id=$1 AND status='RESOLVED')", cmd.TransactionID)
	return result, err
}

func transferClassificationApplies(transactionType, classification string) bool {
	switch transactionType {
	case "UNCLASSIFIED":
		return true
	case "EXPENSE":
		return classification == "ASSET_PURCHASE"
	case "INCOME":
		return classification == "OWN_ACCOUNT" || classification == "HOUSEHOLD_ACCOUNT" || classification == "IGNORE"
	}
	return false
}
