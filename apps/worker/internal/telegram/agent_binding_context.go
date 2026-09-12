package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type agentReviewBinding struct {
	Kind              string
	TargetID          string
	ReviewRequestID   string
	TransactionID     string
	ReviewType        string
	ConversationState string
	MerchantID        string
	TelegramMessageID int64
	AmountIDR         string
	Label             string
}

type agentMerchantLearningBinding struct {
	ReviewRequestID   string
	TransactionID     string
	TelegramMessageID int64
	Merchant          string
	Category          string
}

func isAgentCoreSideEffect(name string) bool {
	switch name {
	case "record_transaction", "record_transaction_batch", "propose_transaction_correction",
		"confirm_pending_action", "cancel_pending_action", "confirm_pending_batch",
		"cancel_pending_batch", "update_pending_batch":
		return true
	default:
		return false
	}
}

// loadAgentReviewBinding resolves the server-owned review target before the model
// is invoked. Exact Telegram reply binding always wins. Without an exact reply,
// exactly one eligible target for the current Telegram chat is required; reviews
// belonging to another household member/chat never participate in ambiguity.
func (p *Processor) loadAgentReviewBinding(ctx context.Context, householdID string, update telegramUpdate) (*agentReviewBinding, any, int, error) {
	if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0 {
		binding, err := p.exactAgentReviewBinding(ctx, householdID, update.Message.Chat.ID, update.Message.ReplyToMessage.MessageID)
		if err != nil {
			return nil, nil, 0, err
		}
		if binding != nil {
			return binding, agentReviewBindingPublic(binding), 1, nil
		}
	}

	rows, err := p.pool.Query(ctx, `
		SELECT kind,target_id,review_request_id,transaction_id,review_type,conversation_state,merchant_id,message_id,amount_idr,label
		FROM (
			SELECT 'TRANSACTION'::text,r.id::text,r.id::text,t.id::text,r.review_type,
			       COALESCE(c.state,''),COALESCE(t.merchant_id::text,''),COALESCE(rr.telegram_message_id,0)::bigint,
			       t.amount::text,COALESCE(t.counterparty_name,t.description,'Transaksi'),r.created_at
			FROM review_request r
			JOIN transaction t ON t.id=r.transaction_id
			JOIN review_conversation c ON c.review_request_id=r.id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND rr.telegram_chat_id=$2
			UNION ALL
			SELECT 'TRANSFER_RECONCILIATION',trc.id::text,r.id::text,'',r.review_type,'','',COALESCE(rr.telegram_message_id,0)::bigint,
			       trc.amount_idr::text,COALESCE(trc.description,'Transfer'),r.created_at
			FROM transfer_reconciliation_case trc
			JOIN review_item ri ON ri.source_event_id=trc.source_event_id
			JOIN review_request r ON r.review_item_id=ri.id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE trc.household_id=$1 AND trc.status='OPEN' AND ri.status IN ('PENDING_SEND','OPEN')
			  AND r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2
			UNION ALL
			SELECT 'WEALTH_OBSERVATION',wo.id::text,r.id::text,'',r.review_type,'','',COALESCE(rr.telegram_message_id,0)::bigint,
			       wo.observed_value_idr::text,trim(wo.institution||' '||wo.account_hint),r.created_at
			FROM wealth_observation wo
			JOIN review_item ri ON ri.wealth_observation_id=wo.id
			JOIN review_request r ON r.review_item_id=ri.id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE wo.household_id=$1 AND wo.status='PENDING' AND ri.status IN ('PENDING_SEND','OPEN')
			  AND r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2
			UNION ALL
			SELECT 'CYCLE_RESIDUAL',crc.id::text,r.id::text,'',r.review_type,'','',COALESCE(rr.telegram_message_id,0)::bigint,
			       crc.basis_residual_idr::text,'Sisa salary cycle',r.created_at
			FROM review_request r
			JOIN review_item ri ON ri.id=r.review_item_id
			JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE r.household_id=$1 AND r.review_type='CYCLE_RESIDUAL_ALLOCATION' AND r.status='OPEN'
			  AND ri.status IN ('PENDING_SEND','OPEN') AND rr.telegram_chat_id=$2
		) candidates(kind,target_id,review_request_id,transaction_id,review_type,conversation_state,merchant_id,message_id,amount_idr,label,sort_at)
		ORDER BY sort_at DESC
		LIMIT 2`, householdID, update.Message.Chat.ID)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	bindings := make([]agentReviewBinding, 0, 2)
	for rows.Next() {
		var binding agentReviewBinding
		if err := rows.Scan(&binding.Kind, &binding.TargetID, &binding.ReviewRequestID, &binding.TransactionID,
			&binding.ReviewType, &binding.ConversationState, &binding.MerchantID, &binding.TelegramMessageID,
			&binding.AmountIDR, &binding.Label); err != nil {
			return nil, nil, 0, err
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	if len(bindings) == 1 {
		return &bindings[0], agentReviewBindingPublic(&bindings[0]), 1, nil
	}
	if len(bindings) > 1 {
		public := make([]map[string]any, 0, len(bindings))
		for index := range bindings {
			public = append(public, agentReviewBindingPublic(&bindings[index]))
		}
		return nil, public, len(bindings), nil
	}
	return nil, nil, 0, nil
}

func (p *Processor) exactAgentReviewBinding(ctx context.Context, householdID string, chatID, messageID int64) (*agentReviewBinding, error) {
	var binding agentReviewBinding
	var residualID, wealthID, transferID string
	err := p.pool.QueryRow(ctx, `
		SELECT r.id::text,COALESCE(r.transaction_id::text,''),r.review_type,COALESCE(c.state,''),
		       COALESCE(t.merchant_id::text,''),COALESCE(ri.cycle_residual_case_id::text,''),
		       COALESCE(ri.wealth_observation_id::text,''),COALESCE(trc.id::text,''),
		       COALESCE(rr.telegram_message_id,0)::bigint,
		       COALESCE(t.amount::text,crc.basis_residual_idr::text,wo.observed_value_idr::text,trc.amount_idr::text,''),
		       COALESCE(t.counterparty_name,t.description,trim(wo.institution||' '||wo.account_hint),trc.description,'Review')
		FROM review_request r
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		LEFT JOIN review_item ri ON ri.id=r.review_item_id
		LEFT JOIN transaction t ON t.id=r.transaction_id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		LEFT JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id
		LEFT JOIN wealth_observation wo ON wo.id=ri.wealth_observation_id AND wo.status='PENDING'
		LEFT JOIN transfer_reconciliation_case trc ON trc.source_event_id=ri.source_event_id AND trc.status='OPEN'
		WHERE r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		LIMIT 1`, householdID, chatID, messageID).Scan(&binding.ReviewRequestID, &binding.TransactionID,
		&binding.ReviewType, &binding.ConversationState, &binding.MerchantID, &residualID, &wealthID, &transferID,
		&binding.TelegramMessageID, &binding.AmountIDR, &binding.Label)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch {
	case residualID != "":
		binding.Kind, binding.TargetID = "CYCLE_RESIDUAL", residualID
	case wealthID != "":
		binding.Kind, binding.TargetID = "WEALTH_OBSERVATION", wealthID
	case transferID != "":
		binding.Kind, binding.TargetID = "TRANSFER_RECONCILIATION", transferID
	case binding.TransactionID != "":
		binding.Kind, binding.TargetID = "TRANSACTION", binding.ReviewRequestID
	default:
		return nil, nil
	}
	return &binding, nil
}

func agentReviewBindingPublic(binding *agentReviewBinding) map[string]any {
	if binding == nil {
		return nil
	}
	out := map[string]any{
		"review_type": binding.ReviewType,
		"review_mode": binding.Kind,
		"amount_idr":  binding.AmountIDR,
		"description": binding.Label,
	}
	if binding.TelegramMessageID != 0 {
		out["binding"] = "reply_or_server_message"
	}
	return out
}

func (p *Processor) loadAgentMerchantLearningBinding(ctx context.Context, householdID string, update telegramUpdate) (*agentMerchantLearningBinding, int, error) {
	base := `SELECT r.id::text,r.transaction_id::text,COALESCE(rr.telegram_message_id,0)::bigint,
		COALESCE(m.normalized_name,''),COALESCE(cat.name,'')
		FROM review_request r
		JOIN review_conversation c ON c.review_request_id=r.id
		JOIN transaction t ON t.id=r.transaction_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		LEFT JOIN merchant m ON m.id=t.merchant_id
		LEFT JOIN category cat ON cat.id=t.category_id
		WHERE r.household_id=$1 AND r.status='OPEN' AND c.state='AWAITING_CONFIRMATION'
		  AND t.status='CONFIRMED' AND rr.telegram_chat_id=$2`
	params := []any{householdID, update.Message.Chat.ID}
	if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0 {
		base += ` AND rr.telegram_message_id=$3`
		params = append(params, update.Message.ReplyToMessage.MessageID)
	}
	base += ` ORDER BY r.created_at DESC LIMIT 2`
	rows, err := p.pool.Query(ctx, base, params...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	bindings := make([]agentMerchantLearningBinding, 0, 2)
	for rows.Next() {
		var binding agentMerchantLearningBinding
		if err := rows.Scan(&binding.ReviewRequestID, &binding.TransactionID, &binding.TelegramMessageID, &binding.Merchant, &binding.Category); err != nil {
			return nil, 0, err
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(bindings) == 1 {
		return &bindings[0], 1, nil
	}
	return nil, len(bindings), nil
}

func agentReviewBindingDebug(binding *agentReviewBinding) string {
	if binding == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s", binding.Kind, binding.TargetID)
}
