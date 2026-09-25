package reviewdomain

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// TransferCommand is one canonical transfer-classification decision for a
// transaction review that is still NEEDS_REVIEW with an unresolved type.
type TransferCommand struct {
	HouseholdID   string
	ActorUserID   string
	TransactionID string
	ReviewItemID  string
	RequestID     string
	Action        string
	// Classification is EXPENSE, OWN_ACCOUNT, HOUSEHOLD_ACCOUNT,
	// INVESTMENT_ACCOUNT, ASSET_PURCHASE, or IGNORE.
	Classification  string
	CategoryID      string
	WealthAccountID string
}

// TransferResult reports the canonical classification outcome.
type TransferResult struct {
	Type            string
	Status          string
	Purpose         string
	WealthAccountID string
	CategoryID      string
	// Counterparty is the stored transaction counterparty, exposed so a surface
	// can default a remembered account display name without re-reading the row.
	Counterparty *string
}

var (
	// ErrTransferNotFound reports a transaction that is no longer an open
	// transfer-classification subject.
	ErrTransferNotFound = errors.New("reviewdomain: transfer review not found")
	// ErrExpenseCategoryRequired reports an EXPENSE classification without a category.
	ErrExpenseCategoryRequired = errors.New("reviewdomain: expense category is required")
	// ErrWealthAccountRequired reports ASSET_PURCHASE without a Wealth Account.
	ErrWealthAccountRequired = errors.New("reviewdomain: asset purchase requires a Wealth Account")
	// ErrInvestmentAccountAmbiguous reports an investment-account match that is not unique.
	ErrInvestmentAccountAmbiguous = errors.New("reviewdomain: investment account requires a deterministic linked wealth account")
	// ErrWealthAccountIncompatible reports a Wealth Account that cannot hold this classification.
	ErrWealthAccountIncompatible = errors.New("reviewdomain: incompatible wealth account")
)

// ClassifyTransferReview applies one canonical transfer classification: it locks
// the subject, resolves Wealth Account/category candidates deterministically,
// mutates the transaction and proposal, refreshes source-event state, and
// completes the review item(s). Web and Telegram both call it (ADR-046).
func ClassifyTransferReview(ctx context.Context, tx pgx.Tx, cmd TransferCommand) (TransferResult, error) {
	var result TransferResult
	var transactionType string
	var counterparty *string
	err := tx.QueryRow(ctx, "SELECT type,counterparty_name FROM transaction WHERE id=$1 AND household_id=$2 AND status='NEEDS_REVIEW' FOR UPDATE", cmd.TransactionID, cmd.HouseholdID).Scan(&transactionType, &counterparty)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrTransferNotFound
	}
	if err != nil {
		return result, err
	}
	if transactionType != "UNCLASSIFIED" && !(transactionType == "EXPENSE" && cmd.Classification == "ASSET_PURCHASE") {
		return result, ErrTransferNotFound
	}
	result.Type, result.Status, result.Purpose = "TRANSFER", "CONFIRMED", "INTERNAL_TRANSFER"
	result.Counterparty = counterparty
	proposalStatus, sourceStatus := "ACCEPTED", "PROCESSED"
	var wealthAccountID, categoryID *string
	if cmd.Classification == "INVESTMENT_ACCOUNT" {
		hint := strings.TrimSpace(cmd.WealthAccountID)
		if counterparty != nil && strings.TrimSpace(*counterparty) != "" && hint == "" {
			hint = strings.TrimSpace(*counterparty)
		}
		var count int
		var linked string
		if err = tx.QueryRow(ctx, "SELECT count(DISTINCT ka.wealth_account_id),COALESCE(min(ka.wealth_account_id::text),'') FROM known_account ka JOIN wealth_account wa ON wa.id=ka.wealth_account_id AND wa.household_id=ka.household_id AND wa.active WHERE ka.household_id=$1 AND ka.active AND ka.relationship='INVESTMENT_ACCOUNT' AND ka.wealth_account_id IS NOT NULL AND lower($2) LIKE '%'||lower(ka.match_hint)", cmd.HouseholdID, hint).Scan(&count, &linked); err != nil {
			return result, err
		}
		if count != 1 {
			return result, ErrInvestmentAccountAmbiguous
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
		proposalStatus, sourceStatus = "REJECTED", "IGNORED"
	}
	if wealthAccountID != nil {
		result.WealthAccountID = *wealthAccountID
	}
	if categoryID != nil {
		result.CategoryID = *categoryID
	}
	if _, err = tx.Exec(ctx, "UPDATE transaction SET type=$2,purpose=$3,related_wealth_account_id=$4,status=$5,category_id=$6,confirmed_at=CASE WHEN $5='CONFIRMED' THEN now() END,voided_at=CASE WHEN $5='VOIDED' THEN now() END,updated_at=now() WHERE id=$1 AND status='NEEDS_REVIEW'", cmd.TransactionID, result.Type, result.Purpose, wealthAccountID, result.Status, categoryID); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "UPDATE transaction_proposal SET proposed_type=$2,proposal_status=$3,category_candidate_id=$4,metadata_json=metadata_json||jsonb_build_object('transfer_classification',$5::text,'purpose',$6::text,'related_wealth_account_id',NULLIF($7,'')::text),updated_at=now() WHERE id IN(SELECT NULLIF(metadata_json->>'proposal_id','')::uuid FROM transaction_evidence WHERE transaction_id=$1)", cmd.TransactionID, result.Type, proposalStatus, categoryID, cmd.Classification, result.Purpose, result.WealthAccountID); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "UPDATE source_event SET processing_status=$2 WHERE id IN(SELECT source_event_id FROM transaction_evidence WHERE transaction_id=$1)", cmd.TransactionID, sourceStatus); err != nil {
		return result, err
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
