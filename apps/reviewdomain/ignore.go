package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// IgnoreCommand dismisses one exact review item that is not bound to a
// transaction. The caller authenticates the actor and binds the interaction
// (for Telegram, the stored card message) before entering this transaction.
type IgnoreCommand struct {
	HouseholdID  string
	ActorUserID  string
	ReviewItemID string
	// ActorType is the canonical audit surface; empty defaults to USER.
	ActorType string
}

var (
	// ErrIgnoreNotAllowed reports a review whose stored contract does not offer
	// IGNORE (for example a cycle residual, whose only dismissal is
	// LEAVE_UNALLOCATED). Surfaces must not render a dismiss button for it.
	ErrIgnoreNotAllowed = errors.New("reviewdomain: this review cannot be ignored")
	// ErrIgnoreTransactionBound reports a transaction-bound review, which is
	// dismissed by RejectTransactionReview so the transaction is voided with it.
	ErrIgnoreTransactionBound = errors.New("reviewdomain: transaction-bound review uses RejectTransactionReview")
)

// IgnoreAllowed reports whether a stored decision offers IGNORE. A decision with
// no stored actions falls back to the kind registry, so a surface-only kind
// such as MISSING_AMOUNT keeps its declared dismissal.
func IgnoreAllowed(reviewType string, decision []byte) bool {
	var stored struct {
		AllowedActions []string `json:"allowedActions"`
	}
	if len(decision) > 0 {
		_ = json.Unmarshal(decision, &stored)
	}
	actions := stored.AllowedActions
	if len(actions) == 0 {
		actions = reviewdec.TelegramActions(reviewType)
		if len(actions) == 0 {
			actions = reviewdec.InboxActions(reviewType)
		}
	}
	return slices.Contains(actions, "IGNORE")
}

// IgnoreReview is the canonical IGNORE for every review kind that is not bound
// to a transaction: a missing-amount screenshot row, a wealth observation, a
// source-bound transfer reconciliation, a financial-email observation, or any
// other source/proposal/document review. It routes to the kind's own canonical
// resolver where one exists and otherwise parks the evidence (never deletes it)
// and completes the item and its Telegram projection, matching the Review Inbox.
func IgnoreReview(ctx context.Context, tx pgx.Tx, cmd IgnoreCommand) error {
	actorType := cmd.ActorType
	if actorType == "" {
		actorType = "USER"
	}
	var kind, status string
	var decision []byte
	var proposal, source, document, transaction, residualCase, wealthObservation, financialObservation *string
	err := tx.QueryRow(ctx, `SELECT review_type,status,decision,proposal_id::text,source_event_id::text,document_id::text,transaction_id::text,cycle_residual_case_id::text,wealth_observation_id::text,financial_email_observation_id::text
		FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, cmd.ReviewItemID, cmd.HouseholdID).
		Scan(&kind, &status, &decision, &proposal, &source, &document, &transaction, &residualCase, &wealthObservation, &financialObservation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	if err != nil {
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return ErrAlreadyResolved
	}
	if transaction != nil {
		return ErrIgnoreTransactionBound
	}
	if residualCase != nil || !IgnoreAllowed(kind, decision) {
		return ErrIgnoreNotAllowed
	}
	if financialObservation != nil && source == nil {
		var bound string
		if err := tx.QueryRow(ctx, `SELECT source_event_id::text FROM financial_email_observation WHERE id=$1 AND household_id=$2`, *financialObservation, cmd.HouseholdID).Scan(&bound); err != nil {
			return ErrAlreadyResolved
		}
		source = &bound
	}
	switch {
	case kind == "MISSING_AMOUNT" && proposal != nil:
		var proposalSource string
		if err := tx.QueryRow(ctx, `SELECT source_event_id::text FROM transaction_proposal WHERE id=$1 AND household_id=$2`, *proposal, cmd.HouseholdID).Scan(&proposalSource); err != nil {
			return ErrAlreadyResolved
		}
		err := IgnoreMissingAmountProposal(ctx, tx, MissingAmountCommand{HouseholdID: cmd.HouseholdID, UserID: cmd.ActorUserID, ReviewItemID: cmd.ReviewItemID, ProposalID: *proposal, SourceEventID: proposalSource, ActorType: actorType})
		if errors.Is(err, ErrMissingAmountReviewInvalid) {
			return ErrAlreadyResolved
		}
		return err
	case (kind == "TRANSFER_CLASSIFICATION" || kind == "FINANCIAL_EMAIL_RESOLUTION") && financialObservation != nil:
		_, err := ResolveFinancialEmailReview(ctx, tx, FinancialEmailCommand{HouseholdID: cmd.HouseholdID, ObservationID: *financialObservation, ReviewItemID: cmd.ReviewItemID, ActorUserID: cmd.ActorUserID, ActorType: actorType, Ignore: true})
		return err
	case kind == "TRANSFER_CLASSIFICATION" && source != nil:
		_, err := ReconcileTransfer(ctx, tx, TransferReconciliationCommand{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, ReviewItemID: cmd.ReviewItemID, Action: "IGNORE", ActorType: actorType})
		if errors.Is(err, ErrTransferCaseUnavailable) {
			// No open case: the evidence is parked through the generic path below.
			break
		}
		return err
	}
	if wealthObservation != nil {
		if err := DismissWealthObservation(ctx, tx, WealthObservationCommand{HouseholdID: cmd.HouseholdID, ObservationID: *wealthObservation, IgnoreFinancialEmail: true}); err != nil && !errors.Is(err, ErrWealthObservationNotFound) {
			return err
		}
	}
	if financialObservation != nil {
		if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2`, *financialObservation, cmd.HouseholdID); err != nil {
			return err
		}
	}
	if proposal != nil {
		if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1 AND household_id=$2 AND proposal_status IN ('PENDING','NEEDS_REVIEW')`, *proposal, cmd.HouseholdID); err != nil {
			return err
		}
	}
	if source != nil {
		if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status=CASE WHEN $3::uuid IS NULL THEN 'IGNORED' WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status IN ('PENDING','REVIEW')) THEN 'NEEDS_REVIEW' WHEN EXISTS(SELECT 1 FROM financial_email_observation WHERE source_event_id=$1 AND status='APPLIED') THEN 'PROCESSED' ELSE 'IGNORED' END WHERE id=$1 AND household_id=$2`, *source, cmd.HouseholdID, financialObservation); err != nil {
			return err
		}
	}
	if document != nil {
		if _, err := tx.Exec(ctx, `UPDATE document SET status='NEEDS_REVIEW',updated_at=now() WHERE id=$1 AND household_id=$2`, *document, cmd.HouseholdID); err != nil {
			return err
		}
	}
	result, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='IGNORE',resolution_values='{}'::jsonb,updated_at=now() WHERE id=$1 AND household_id=$3 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, cmd.ActorUserID, cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE review_item_id=$1 AND household_id=$2 AND status IN ('OPEN','PENDING_SEND')`, cmd.ReviewItemID, cmd.HouseholdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1 AND household_id=$2)`, cmd.ReviewItemID, cmd.HouseholdID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,$3,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action','IGNORE','review_type',$5::text))`, cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.ReviewItemID, kind)
	return err
}
