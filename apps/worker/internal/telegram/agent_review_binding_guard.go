package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// lockAgentReviewBindingTx revalidates the server-owned Telegram review binding
// inside the same transaction that will mutate the bound finance subject. This
// prevents target drift between context construction and mutation execution.
func (p *Processor) lockAgentReviewBindingTx(ctx context.Context, tx pgx.Tx, state *agentState, binding *agentReviewBinding, expectedKind string) (bool, error) {
	if binding == nil || binding.Kind != expectedKind || binding.TargetID == "" || binding.ReviewRequestID == "" {
		return false, nil
	}
	messageID := binding.TelegramMessageID
	if state.Update.Message.ReplyToMessage != nil && state.Update.Message.ReplyToMessage.MessageID != 0 {
		if messageID == 0 || messageID != state.Update.Message.ReplyToMessage.MessageID {
			return false, nil
		}
	}

	var lockedID string
	var err error
	switch expectedKind {
	case "TRANSFER_RECONCILIATION":
		err = tx.QueryRow(ctx, `
			SELECT r.id::text
			FROM review_request r
			JOIN review_item ri ON ri.id=r.review_item_id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			JOIN transfer_reconciliation_case trc ON (
				(ri.source_event_id IS NOT NULL AND trc.source_event_id=ri.source_event_id)
				OR (ri.financial_email_observation_id IS NOT NULL AND trc.financial_email_observation_id=ri.financial_email_observation_id)
			)
			WHERE r.id=$1 AND r.household_id=$2 AND r.status='OPEN'
			  AND ri.household_id=$2 AND ri.status IN ('PENDING_SEND','OPEN')
			  AND trc.id=$3 AND trc.household_id=$2 AND trc.status='OPEN'
			  AND rr.telegram_chat_id=$4
			  AND ($5::bigint=0 OR rr.telegram_message_id=$5)
			FOR UPDATE OF r,ri,rr,trc`, binding.ReviewRequestID, state.HouseholdID, binding.TargetID, state.Update.Message.Chat.ID, messageID).Scan(&lockedID)
	case "WEALTH_OBSERVATION":
		err = tx.QueryRow(ctx, `
			SELECT r.id::text
			FROM review_request r
			JOIN review_item ri ON ri.id=r.review_item_id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			JOIN wealth_observation wo ON wo.id=ri.wealth_observation_id
			WHERE r.id=$1 AND r.household_id=$2 AND r.status='OPEN'
			  AND ri.household_id=$2 AND ri.status IN ('PENDING_SEND','OPEN')
			  AND wo.id=$3 AND wo.household_id=$2 AND wo.status='PENDING'
			  AND rr.telegram_chat_id=$4
			  AND ($5::bigint=0 OR rr.telegram_message_id=$5)
			FOR UPDATE OF r,ri,rr,wo`, binding.ReviewRequestID, state.HouseholdID, binding.TargetID, state.Update.Message.Chat.ID, messageID).Scan(&lockedID)
	default:
		return false, fmt.Errorf("unsupported bound review kind %q", expectedKind)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return lockedID == binding.ReviewRequestID, nil
}

func (p *Processor) loadBoundWealthObservationTx(ctx context.Context, tx pgx.Tx, state *agentState, binding *agentReviewBinding) (resolved, institution, hint, originalSource string, valid bool, err error) {
	valid, err = p.lockAgentReviewBindingTx(ctx, tx, state, binding, "WEALTH_OBSERVATION")
	if err != nil || !valid {
		return "", "", "", "", valid, err
	}
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(wo.resolved_wealth_account_id::text,''),wo.institution,wo.account_hint,
		       COALESCE(d.source_event_id::text,feo.source_event_id::text,'')
		FROM wealth_observation wo
		LEFT JOIN document d ON d.id=wo.document_id
		LEFT JOIN financial_email_observation feo ON feo.id=wo.financial_email_observation_id
		WHERE wo.id=$1 AND wo.household_id=$2 AND wo.status='PENDING'
		FOR UPDATE OF wo`, binding.TargetID, state.HouseholdID).Scan(&resolved, &institution, &hint, &originalSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", "", false, nil
	}
	return resolved, institution, hint, originalSource, true, err
}

func resolveAgentBoundReviewRequestTx(ctx context.Context, tx pgx.Tx, binding *agentReviewBinding, userID, resolution string) error {
	if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action=$3,updated_at=now() WHERE id=(SELECT review_item_id FROM review_request WHERE id=$1) AND status IN ('PENDING_SEND','OPEN')`, binding.ReviewRequestID, userID, resolution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE review_request SET status='RESOLVED',resolved_at=now() WHERE id=$1 AND status='OPEN'`, binding.ReviewRequestID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE review_conversation SET state='RESOLVED',last_message_at=now(),updated_at=now() WHERE review_request_id=$1`, binding.ReviewRequestID)
	return err
}

func (p *Processor) agentDismissBoundTransferReconciliation(ctx context.Context, state *agentState, callID string, binding *agentReviewBinding) (agentToolResult, bool, error) {
	result := agentToolResult{CallID: callID, Tool: "resolve_review", Class: agentToolSideEffect}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, true, err
	}
	defer tx.Rollback(ctx)
	valid, err := p.lockAgentReviewBindingTx(ctx, tx, state, binding, "TRANSFER_RECONCILIATION")
	if err != nil {
		return result, true, err
	}
	if !valid {
		result.Status = "STALE_REVIEW_BINDING"
		return result, true, nil
	}
	var originalSource, amount, purpose string
	if err = tx.QueryRow(ctx, `SELECT source_event_id::text,amount_idr::text,proposed_purpose FROM transfer_reconciliation_case WHERE id=$1 AND household_id=$2 AND status='OPEN' FOR UPDATE`, binding.TargetID, state.HouseholdID).Scan(&originalSource, &amount, &purpose); err != nil {
		return result, true, err
	}
	var userID string
	if err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE transfer_reconciliation_case SET status='DISMISSED',resolved_at=now(),resolved_by_user_id=$2,updated_at=now() WHERE id=$1 AND household_id=$3 AND status='OPEN'`, binding.TargetID, userID, state.HouseholdID); err != nil {
		return result, true, err
	}
	if err = resolveAgentBoundReviewRequestTx(ctx, tx, binding, userID, "IGNORED"); err != nil {
		return result, true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='IGNORED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, originalSource, state.HouseholdID); err != nil {
		return result, true, err
	}
	if state.SourceEventID != originalSource {
		if _, err = tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-conversational-agent',parser_version='1' WHERE id=$1 AND household_id=$2`, state.SourceEventID, state.HouseholdID); err != nil {
			return result, true, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'DISMISS_TRANSFER_RECONCILIATION','transfer_reconciliation_case',$3,jsonb_build_object('review_request_id',$4::uuid,'resolution','IGNORE','agent_sprint',1))`, state.HouseholdID, userID, binding.TargetID, binding.ReviewRequestID); err != nil {
		return result, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, true, err
	}
	result.Status = "RESOLVED"
	result.Mutation = map[string]any{"action": "TRANSFER_RECONCILIATION_IGNORED", "resolution": "IGNORE", "amount_idr": amount, "purpose": purpose}
	return result, true, nil
}
