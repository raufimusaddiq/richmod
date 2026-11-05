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

	// Without an exact reply, only live cards count (loadLiveReviewCards). This
	// reports the set; whether free text answers one of them is decided by
	// matchTypedCardAnswer, never by the model.
	bindings, err := p.loadLiveReviewCards(ctx, householdID, update.Message.Chat.ID)
	if err != nil {
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
	var residualID, wealthID, transferID, bankItemID string
	err := p.pool.QueryRow(ctx, `
		SELECT r.id::text,COALESCE(r.transaction_id::text,''),r.review_type,COALESCE(c.state,''),
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
		LIMIT 1`, householdID, chatID, messageID).Scan(&binding.ReviewRequestID, &binding.TransactionID,
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
		// A bank email whose facts could not be verified: no transaction exists
		// yet, the household supplies the amount and time.
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
		// review_mode is the bound canonical subject/executor (binding.Kind) and
		// stays separate from the semantic action vocabulary, which is keyed by
		// review_type. Conflating them exposed generic actions for a typed review.
		"review_mode": binding.Kind,
		"amount_idr":  binding.AmountIDR,
		"description": binding.Label,
	}
	if binding.TelegramMessageID != 0 {
		out["binding"] = "reply_or_server_message"
	}
	// Name the one value the card asked the household for, so the model puts the
	// reply in that argument instead of guessing.
	if field := awaitedReviewField(binding.ConversationState); field != "" {
		out["awaiting_field"] = field
	}
	if binding.Label != "" {
		out["description"] = untrustedField(binding.Label)
	}
	if binding.Kind == "BANK_FACTS" {
		// The card asks for both values at once; COMPLETE_BANK_FACTS carries them.
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

func (p *Processor) loadAgentMerchantLearningBinding(ctx context.Context, householdID string, update telegramUpdate) (*agentMerchantLearningBinding, int, error) {
	base := `SELECT r.id::text,r.transaction_id::text,COALESCE(rr.telegram_message_id,0)::bigint,
		COALESCE(m.normalized_name,''),COALESCE(cat.name,'')
		FROM review_request r
		JOIN review_conversation c ON c.review_request_id=r.id
		JOIN transaction t ON t.id=r.transaction_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		LEFT JOIN merchant m ON m.id=t.merchant_id
		LEFT JOIN category cat ON cat.id=t.category_id
		WHERE r.household_id=$1 AND c.state='AWAITING_MERCHANT_DECISION'
		  AND t.status='CONFIRMED' AND rr.telegram_chat_id=$2`
	params := []any{householdID, update.Message.Chat.ID}
	if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0 {
		// A reply may target the separate learning question or, for a question
		// delivered as an edit of the card, the card itself.
		base += ` AND (rr.merchant_learning_message_id=$3 OR rr.telegram_message_id=$3)`
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
