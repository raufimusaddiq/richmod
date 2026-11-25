package telegram

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const wealthObservationBindingKind = "WEALTH_OBSERVATION"

func (p *Processor) lockWealthObservationBindingTx(ctx context.Context, tx pgx.Tx, state *agentState, binding *agentReviewBinding) (bool, error) {
	if binding == nil || binding.Kind != wealthObservationBindingKind || binding.TargetID == "" || binding.ReviewRequestID == "" {
		return false, nil
	}
	messageID := binding.TelegramMessageID
	if state.Update.Message.ReplyToMessage != nil && state.Update.Message.ReplyToMessage.MessageID != 0 {
		if messageID == 0 {
			return false, nil
		}
		if messageID != state.Update.Message.ReplyToMessage.MessageID {
			bound, err := p.replyBoundToReviewCard(ctx, state, messageID)
			if err != nil || !bound {
				return false, err
			}
		}
	}
	var lockedID string
	err := tx.QueryRow(ctx, `
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
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return lockedID == binding.ReviewRequestID, nil
}

func (p *Processor) replyBoundToReviewCard(ctx context.Context, state *agentState, cardMessageID int64) (bool, error) {
	document, found, err := p.resolveReplyEvidence(ctx, state.HouseholdID, state.Update)
	if err != nil || !found {
		return false, err
	}
	card, err := p.reviewCardMessageForDocument(ctx, state.HouseholdID, state.Update.Message.Chat.ID, document)
	if err != nil {
		return false, err
	}
	return card != 0 && card == cardMessageID, nil
}

func (p *Processor) loadBoundWealthObservationTx(ctx context.Context, tx pgx.Tx, state *agentState, binding *agentReviewBinding) (resolved, institution, hint, originalSource string, valid bool, err error) {
	valid, err = p.lockWealthObservationBindingTx(ctx, tx, state, binding)
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
