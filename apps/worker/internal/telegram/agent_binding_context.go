package telegram

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type agentReviewBinding struct {
	Kind              string
	TargetID          string
	ReviewRequestID   string
	ReviewItemID      string
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
		"confirm_pending_action", "cancel_pending_action", "pending_batch_decision", "dispute_evidence_link":
		return true
	default:
		return false
	}
}

func (p *Processor) exactAgentReviewBinding(ctx context.Context, householdID string, chatID, messageID int64) (*agentReviewBinding, error) {
	var binding agentReviewBinding
	var residualID, wealthID, transferID, bankItemID string
	err := p.pool.QueryRow(ctx, `
		SELECT r.id::text,r.review_item_id::text,COALESCE(r.transaction_id::text,''),r.review_type,COALESCE(c.state,''),
		       COALESCE(t.merchant_id::text,''),COALESCE(ri.cycle_residual_case_id::text,''),
		       COALESCE(ri.wealth_observation_id::text,''),COALESCE(trc.id::text,''),
		       COALESCE(rr.telegram_message_id,0)::bigint,
		       COALESCE(t.amount::text,crc.basis_residual_idr::text,wo.observed_value_idr::text,trc.amount_idr::text,''),
		       COALESCE(t.counterparty_name,t.description,trim(wo.institution||' '||wo.account_hint),trc.description,'Review'),
		       CASE WHEN ri.review_type='UNKNOWN_BANK_TEMPLATE' AND ri.transaction_id IS NULL AND ri.source_event_id IS NOT NULL
		            AND ri.status IN ('OPEN','PENDING_SEND') THEN ri.id::text ELSE '' END
		FROM review_request r
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		LEFT JOIN review_item ri ON ri.id=r.review_item_id
		LEFT JOIN transaction t ON t.id=r.transaction_id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		LEFT JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id
		LEFT JOIN wealth_observation wo ON wo.id=ri.wealth_observation_id AND wo.status='PENDING'
		LEFT JOIN transfer_reconciliation_case trc ON (
			(ri.source_event_id IS NOT NULL AND trc.source_event_id=ri.source_event_id)
			OR (ri.financial_email_observation_id IS NOT NULL AND trc.financial_email_observation_id=ri.financial_email_observation_id)
		) AND trc.status='OPEN'
		WHERE r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2 AND rr.telegram_message_id=$3
		LIMIT 1`, householdID, chatID, messageID).Scan(&binding.ReviewRequestID, &binding.ReviewItemID, &binding.TransactionID,
		&binding.ReviewType, &binding.ConversationState, &binding.MerchantID, &residualID, &wealthID, &transferID,
		&binding.TelegramMessageID, &binding.AmountIDR, &binding.Label, &bankItemID)
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
	case bankItemID != "":
		binding.Kind, binding.TargetID, binding.Label = "BANK_FACTS", bankItemID, "Email bank"
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
	if field := awaitedReviewField(binding.ConversationState); field != "" {
		out["awaiting_field"] = field
	}
	if binding.Label != "" {
		out["description"] = untrustedField(binding.Label)
	}
	if binding.Kind == "BANK_FACTS" {
		out["awaiting_field"] = "amount_idr and transaction_at"
	}
	return out
}

func awaitedReviewField(conversationState string) string {
	switch conversationState {
	case "AWAITING_DATE":
		return "transaction_at"
	case "AWAITING_MERCHANT":
		return "merchant"
	case "AWAITING_CATEGORY":
		return "category_slug"
	}
	return ""
}
