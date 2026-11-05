package reviewdomain

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

// IgnoreCommand dismisses or acknowledges one exact review item. The caller
// authenticates the actor and binds the interaction (for Telegram, the stored
// card message) before entering this transaction.
type IgnoreCommand struct {
	HouseholdID  string
	ActorUserID  string
	ReviewItemID string
	// ActorType is the canonical audit surface; empty defaults to USER.
	ActorType string
	// Values is recorded as the resolution values of a generic dismissal.
	Values []byte
}

var (
	// ErrUseTransactionAction reports a transaction review whose transaction is
	// neither awaiting review nor final (for example PENDING): neither a
	// rejection nor an acknowledgement is correct, so nothing is changed.
	ErrUseTransactionAction = errors.New("reviewdomain: use the transaction review action")
	// ErrIgnoreNotAllowed reports a review whose stored contract does not offer
	// IGNORE (for example a cycle residual, whose only dismissal is
	// LEAVE_UNALLOCATED). Surfaces must not render a dismiss button for it.
	ErrIgnoreNotAllowed = errors.New("reviewdomain: this review cannot be ignored")
	// ErrIgnoreTransactionBound reports a transaction-bound review passed to
	// IgnoreReview; IgnoreReviewItem handles those (reject or acknowledge).
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

// IgnoreReviewItem is the IGNORE every surface's generic dismissal shares
// (ADR-046). A transaction-bound review is handled by the transaction lane: an
// open transaction is rejected (RejectTransactionReview), and a review whose
// transaction is already final is only acknowledged (NO_LONGER_APPLICABLE)
// without touching it. Every other review goes through IgnoreReview.
func IgnoreReviewItem(ctx context.Context, tx pgx.Tx, cmd IgnoreCommand) error {
	var status string
	var transaction *string
	err := tx.QueryRow(ctx, `SELECT status,transaction_id::text FROM review_item WHERE id=$1 AND household_id=$2 FOR UPDATE`, cmd.ReviewItemID, cmd.HouseholdID).Scan(&status, &transaction)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	if err != nil {
		return err
	}
	if status != "OPEN" && status != "PENDING_SEND" {
		return ErrAlreadyResolved
	}
	if transaction == nil {
		return IgnoreReview(ctx, tx, cmd)
	}
	var txStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1 AND household_id=$2`, *transaction, cmd.HouseholdID).Scan(&txStatus); err != nil {
		return notFoundAsResolved(err)
	}
	if txStatus == "NEEDS_REVIEW" {
		// Dismissing an open transaction review is the canonical rejection
		// (Web "Abaikan", Telegram ignore): offered and accepted are one rule.
		return RejectTransactionReview(ctx, tx, RejectCommand{HouseholdID: cmd.HouseholdID, ActorUserID: cmd.ActorUserID, TransactionID: *transaction, ReviewItemID: cmd.ReviewItemID, Action: "IGNORE"})
	}
	if txStatus != "CONFIRMED" && txStatus != "VOIDED" {
		return ErrUseTransactionAction
	}
	if err := resolveItem(ctx, tx, cmd, "NO_LONGER_APPLICABLE", []byte(`{"reason":"transaction_already_final"}`)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,NULLIF($3,'')::uuid,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action','IGNORE','reason','transaction_already_final'))`, cmd.HouseholdID, actorTypeOf(cmd), cmd.ActorUserID, cmd.ReviewItemID); err != nil {
		return err
	}
	// The acknowledged item may have been the last thing holding the
	// transaction's sources in review.
	return RefreshTransactionLifecycle(ctx, tx, cmd.HouseholdID, *transaction)
}

// IgnoreReview is the canonical IGNORE for every review kind that is not bound
// to a transaction: a missing-amount screenshot row, a wealth observation, a
// source-bound transfer reconciliation, a financial-email observation, or any
// other source/proposal/document review. It routes to the kind's own canonical
// resolver where one exists; otherwise it parks the evidence (never deletes it:
// observation IGNORED/DISMISSED, proposal REJECTED), completes the item and its
// Telegram projection, and settles every evidence source through the shared
// lifecycle (MarkSourceIgnored), which also settles the document.
func IgnoreReview(ctx context.Context, tx pgx.Tx, cmd IgnoreCommand) error {
	actorType := actorTypeOf(cmd)
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
	switch {
	case kind == "MISSING_AMOUNT" && proposal != nil:
		var proposalSource string
		if err := tx.QueryRow(ctx, `SELECT source_event_id::text FROM transaction_proposal WHERE id=$1 AND household_id=$2`, *proposal, cmd.HouseholdID).Scan(&proposalSource); err != nil {
			return notFoundAsResolved(err)
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
		if !errors.Is(err, ErrTransferCaseUnavailable) {
			return err
		}
		// No open case: the evidence is parked through the generic path below.
	}
	if financialObservation != nil {
		if _, err := tx.Exec(ctx, `UPDATE financial_email_observation SET status='IGNORED',updated_at=now() WHERE id=$1 AND household_id=$2 AND status IN ('PENDING','REVIEW')`, *financialObservation, cmd.HouseholdID); err != nil {
			return err
		}
	}
	if wealthObservation != nil {
		if err := DismissWealthObservation(ctx, tx, WealthObservationCommand{HouseholdID: cmd.HouseholdID, ObservationID: *wealthObservation, IgnoreFinancialEmail: true}); err != nil && !errors.Is(err, ErrWealthObservationNotFound) {
			return err
		}
	}
	if proposal != nil {
		if _, err := tx.Exec(ctx, `UPDATE transaction_proposal SET proposal_status='REJECTED',updated_at=now() WHERE id=$1 AND household_id=$2 AND proposal_status IN ('PENDING','NEEDS_REVIEW')`, *proposal, cmd.HouseholdID); err != nil {
			return err
		}
	}
	values := cmd.Values
	if len(values) == 0 {
		values = []byte(`{}`)
	}
	if err := resolveItem(ctx, tx, cmd, "IGNORE", values); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,$2,NULLIF($3,'')::uuid,'RESOLVE_REVIEW','review_item',$4,jsonb_build_object('action','IGNORE','review_type',$5::text))`, cmd.HouseholdID, actorType, cmd.ActorUserID, cmd.ReviewItemID, kind); err != nil {
		return err
	}
	sources, err := reviewItemSources(ctx, tx, cmd.HouseholdID, proposal, source, document, wealthObservation, financialObservation)
	if err != nil {
		return err
	}
	for _, id := range sources {
		if err := MarkSourceIgnored(ctx, tx, cmd.HouseholdID, id); err != nil {
			return err
		}
	}
	return nil
}

func actorTypeOf(cmd IgnoreCommand) string {
	if cmd.ActorType == "" {
		return "USER"
	}
	return cmd.ActorType
}

// notFoundAsResolved reports a vanished binding as an already-resolved review;
// any other error is returned as is, so a transient failure retries instead of
// telling the household the review was closed.
func notFoundAsResolved(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyResolved
	}
	return err
}

func resolveItem(ctx context.Context, tx pgx.Tx, cmd IgnoreCommand, action string, values []byte) error {
	if !json.Valid(values) {
		values = []byte(`{}`)
	}
	result, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=NULLIF($2,'')::uuid,resolution_action=$3,resolution_values=$4::jsonb,updated_at=now() WHERE id=$1 AND household_id=$5 AND status IN ('PENDING_SEND','OPEN')`, cmd.ReviewItemID, cmd.ActorUserID, action, string(values), cmd.HouseholdID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, resolveRequestByItemSQL, []string{cmd.ReviewItemID}, cmd.HouseholdID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id=$1 AND household_id=$2)`, cmd.ReviewItemID, cmd.HouseholdID)
	return err
}

// reviewItemSources returns the evidence source events one review is about.
func reviewItemSources(ctx context.Context, tx pgx.Tx, householdID string, proposal, source, document, wealth, financial *string) ([]string, error) {
	var out []string
	add := func(id string) {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if source != nil {
		add(*source)
	}
	lookups := []struct {
		id  *string
		sql string
	}{
		{proposal, `SELECT source_event_id::text FROM transaction_proposal WHERE id=$1 AND household_id=$2`},
		{document, `SELECT source_event_id::text FROM document WHERE id=$1 AND household_id=$2`},
		{financial, `SELECT source_event_id::text FROM financial_email_observation WHERE id=$1 AND household_id=$2`},
		{wealth, `SELECT COALESCE((SELECT d.source_event_id::text FROM document d WHERE d.id=wo.document_id),(SELECT f.source_event_id::text FROM financial_email_observation f WHERE f.id=wo.financial_email_observation_id),'') FROM wealth_observation wo WHERE wo.id=$1 AND wo.household_id=$2`},
	}
	for _, lookup := range lookups {
		if lookup.id == nil {
			continue
		}
		var id string
		if err := tx.QueryRow(ctx, lookup.sql, *lookup.id, householdID).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		add(id)
	}
	return out, nil
}
