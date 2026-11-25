package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

type interactionKind string

const (
	interactionNone             interactionKind = "NONE"
	interactionCardStep         interactionKind = "CARD_STEP"
	interactionMerchantLearning interactionKind = "MERCHANT_LEARNING"
	interactionEvidenceNotice   interactionKind = "EVIDENCE_NOTICE"
	interactionAmbiguous        interactionKind = "AMBIGUOUS"
	interactionClosedCard       interactionKind = "CLOSED_CARD"
	interactionAnswerOrNew      interactionKind = "ANSWER_OR_NEW"
)

type interactionSource string

const (
	sourceTap         interactionSource = "TAP"
	sourceReply       interactionSource = "REPLY"
	sourceThreadReply interactionSource = "THREAD_REPLY"
	sourcePlain       interactionSource = "PLAIN"
	sourceLabelText   interactionSource = "LABEL_TEXT"
)

const (
	replyThreadMaxHops      = 6
	interactionParserChoice = "telegram-interaction-choice"
	interactionParserAnswer = "interaction-answer"
	interactionParserNew    = "interaction-new"
	maxLabelTextRunes       = 40
)

type interactionCard struct {
	ReviewRequestID string
	ReviewItemID    string
	ReviewType      string
	State           string
	ChatID          int64
	MessageID       int64
	DeliveredText   string
	Binding         *agentReviewBinding
}

type interactionResult struct {
	Kind       interactionKind
	Source     interactionSource
	Card       interactionCard
	Update     telegramUpdate
	Candidates []agentReviewBinding
	OpenCards  []agentReviewBinding
	Answer     typedCardAnswer
	ButtonData string
	Unmatched  bool
	Bound      bool
}

type messageStop string

const (
	stopNothing          messageStop = ""
	stopLiveCard         messageStop = "LIVE_CARD"
	stopClosedCard       messageStop = "CLOSED_CARD"
	stopMerchantLearning messageStop = "MERCHANT_LEARNING"
	stopEvidence         messageStop = "EVIDENCE"
)

type classifiedMessage struct {
	Stop messageStop
	Card interactionCard
}

func replyToMessage(messageID int64) *struct {
	MessageID int64 `json:"message_id"`
} {
	return &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: messageID}
}

func (p *Processor) resolveInteraction(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, override string) (interactionResult, error) {
	result := interactionResult{Kind: interactionNone, Update: update}
	switch {
	case update.CallbackQuery != nil:
		return p.resolveTap(ctx, householdID, update)
	case update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0:
		return p.resolveReply(ctx, householdID, update)
	}
	return p.resolvePlain(ctx, householdID, update, override, result)
}

func navigationCallback(data string) bool {
	switch data {
	case "review:edit", "review:merchant", "review:description", "review:category":
		return true
	}
	for _, prefix := range []string{"review:catpage:", "review:acctpage:", "review:fepage:", "review:bankpage:", "review:duppage:", "review:investpage:", "review:cycpage:"} {
		if strings.HasPrefix(data, prefix) {
			return true
		}
	}
	return false
}

func (p *Processor) resolveTap(ctx context.Context, householdID string, update telegramUpdate) (interactionResult, error) {
	result := interactionResult{Kind: interactionNone, Update: update}
	message := update.CallbackQuery.Message
	classified, err := p.classifyMessage(ctx, householdID, message.Chat.ID, message.MessageID)
	if err != nil {
		return result, err
	}
	if classified.Stop == stopLiveCard {
		result.Kind, result.Source, result.Card = interactionCardStep, sourceTap, classified.Card
	}
	return result, nil
}

func (p *Processor) resolveReply(ctx context.Context, householdID string, update telegramUpdate) (interactionResult, error) {
	result := interactionResult{Kind: interactionNone, Update: update}
	chatID := update.Message.Chat.ID
	target := update.Message.ReplyToMessage.MessageID
	visited := map[int64]bool{}
	for hop := 0; hop <= replyThreadMaxHops; hop++ {
		visited[target] = true
		classified, err := p.classifyMessage(ctx, householdID, chatID, target)
		if err != nil {
			return result, err
		}
		switch classified.Stop {
		case stopLiveCard:
			return p.landOnCard(ctx, householdID, update, classified.Card, hop, result)
		case stopClosedCard:
			result.Kind, result.Card = interactionClosedCard, classified.Card
			return result, nil
		case stopMerchantLearning:
			if hop == 0 {
				result.Kind = interactionMerchantLearning
			}
			return result, nil
		case stopEvidence:
			if hop == 0 {
				result.Kind = interactionEvidenceNotice
			}
			return result, nil
		}
		parent, err := p.replyThreadParent(ctx, householdID, chatID, target)
		if err != nil {
			return result, err
		}
		if parent == 0 || visited[parent] {
			break
		}
		target = parent
	}
	result.Unmatched = true
	return result, nil
}

func (p *Processor) landOnCard(ctx context.Context, householdID string, update telegramUpdate, card interactionCard, hop int, result interactionResult) (interactionResult, error) {
	source := sourceReply
	if hop > 0 {
		source = sourceThreadReply
	}
	if card.MessageID != update.Message.ReplyToMessage.MessageID {
		update.Message.ReplyToMessage = replyToMessage(card.MessageID)
		slog.Default().Info("reply bound to its review card", "household_id", householdID, "chat_id", card.ChatID, "card_message_id", card.MessageID, "hops", hop)
	}
	if err := p.renewExpiredReviewProjection(ctx, householdID, update); err != nil {
		return result, err
	}
	binding, err := p.exactAgentReviewBinding(ctx, householdID, card.ChatID, card.MessageID)
	if err != nil {
		return result, err
	}
	card.Binding = binding
	result.Kind, result.Source, result.Card, result.Update = interactionCardStep, source, card, update
	if data, ok := p.typedButtonLabel(ctx, householdID, update, card); ok {
		result.Source, result.ButtonData = sourceLabelText, data
	}
	return result, nil
}

func (p *Processor) classifyMessage(ctx context.Context, householdID string, chatID, messageID int64) (classifiedMessage, error) {
	var out classifiedMessage
	var requestStatus, itemStatus, conversation string
	var current int64
	var learningMessage bool
	card := &out.Card
	err := p.pool.QueryRow(ctx, `SELECT r.id::text,ri.id::text,ri.review_type,COALESCE(c.state,''),r.status,ri.status,
			COALESCE(rr.telegram_message_id,0),COALESCE(rr.delivered_text,''),COALESCE(rr.merchant_learning_message_id=$3,false)
		FROM review_request_recipient rr
		JOIN review_request r ON r.id=rr.review_request_id
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2
		  AND (rr.telegram_message_id=$3 OR rr.merchant_learning_message_id=$3
		       OR EXISTS(SELECT 1 FROM review_request_message m WHERE m.recipient_id=rr.id AND m.telegram_chat_id=$2 AND m.telegram_message_id=$3))
		ORDER BY (rr.telegram_message_id=$3) DESC,(r.status='OPEN' AND ri.status IN ('OPEN','PENDING_SEND')) DESC,r.created_at DESC LIMIT 1`, householdID, chatID, messageID).
		Scan(&card.ReviewRequestID, &card.ReviewItemID, &card.ReviewType, &conversation, &requestStatus, &itemStatus, &current, &card.DeliveredText, &learningMessage)
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		return p.classifyForeignMessage(ctx, householdID, chatID, messageID)
	default:
		return out, fmt.Errorf("classify Telegram message: %w", err)
	}
	card.ChatID, card.MessageID, card.State = chatID, current, conversation
	switch {
	case !learningMessage && conversation == "AWAITING_MERCHANT_DECISION" && engineReviewType(card.ReviewType) && current == messageID:
		out.Stop = stopLiveCard
	case learningMessage || conversation == "AWAITING_MERCHANT_DECISION":
		out.Stop = stopMerchantLearning
	case (requestStatus == "OPEN" || requestStatus == "EXPIRED") && (itemStatus == "OPEN" || itemStatus == "PENDING_SEND") && current != 0:
		out.Stop = stopLiveCard
	default:
		out.Stop = stopClosedCard
	}
	return out, nil
}

func (p *Processor) classifyForeignMessage(ctx context.Context, householdID string, chatID, messageID int64) (classifiedMessage, error) {
	var evidence bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_message_binding b WHERE b.household_id=$1 AND b.telegram_chat_id=$2 AND b.telegram_message_id=$3)
		OR EXISTS(SELECT 1 FROM source_event s WHERE s.household_id=$1 AND s.source_type='TELEGRAM_IMAGE' AND s.telegram_chat_id=$2 AND s.telegram_message_id=$3)`,
		householdID, chatID, messageID).Scan(&evidence)
	if err != nil {
		return classifiedMessage{}, fmt.Errorf("classify Telegram evidence message: %w", err)
	}
	if evidence {
		return classifiedMessage{Stop: stopEvidence}, nil
	}
	return classifiedMessage{}, nil
}

func (p *Processor) replyThreadParent(ctx context.Context, householdID string, chatID, messageID int64) (int64, error) {
	var parent int64
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(
		(SELECT NULLIF(j.payload_json->>'reply_to_message_id','')::bigint FROM telegram_outbound_delivery d JOIN job j ON j.id=d.job_id
			WHERE d.telegram_chat_id=$2 AND d.telegram_message_id=$3 AND (d.household_id IS NULL OR d.household_id=$1) AND (j.household_id IS NULL OR j.household_id=$1)
			  AND j.payload_json->>'chat_id'=$2::text
			ORDER BY d.created_at DESC LIMIT 1),
		(SELECT NULLIF(pl.payload_json#>>'{message,reply_to_message,message_id}','')::bigint FROM source_event s JOIN source_event_payload pl ON pl.source_event_id=s.id
			WHERE s.household_id=$1 AND s.source_type='TELEGRAM_TEXT' AND s.received_at>now()-interval '30 days'
			  AND s.telegram_chat_id=$2 AND s.telegram_message_id=$3
			ORDER BY s.received_at DESC LIMIT 1),
		0)`, householdID, chatID, messageID).Scan(&parent)
	if err != nil {
		return 0, fmt.Errorf("follow reply thread: %w", err)
	}
	return parent, nil
}

func (p *Processor) resolvePlain(ctx context.Context, householdID string, update telegramUpdate, override string, result interactionResult) (interactionResult, error) {
	text := strings.TrimSpace(update.Message.Text)
	cards, err := p.loadWaitingCards(ctx, householdID, update.Message.Chat.ID)
	if err != nil {
		return result, err
	}
	for index := range cards {
		if cards[index].Kind != cardKindProposal {
			result.OpenCards = append(result.OpenCards, cards[index])
		}
	}
	if override != interactionParserNew && text != "" && isYesNoAnswer(text) {
		blocked, err := p.plainAnswerBlocked(ctx, householdID, update)
		if err != nil {
			return result, err
		}
		if card, ok := yesNoReshowCard(cards); ok && !blocked {
			result = p.cardStepResult(result, update, card, sourcePlain, "")
			result.Bound = true
			return result, nil
		}
		if _, typed := singleTypedCard(cards); !typed && !blocked {
			if card, ok, err := p.rememberCard(ctx, householdID, update.Message.Chat.ID); err != nil {
				return result, err
			} else if ok {
				result.Kind, result.Source, result.Card, result.Update = interactionCardStep, sourcePlain, card, update
				return result, nil
			}
		}
	}
	if override == interactionParserNew || len(cards) == 0 || text == "" {
		return result, nil
	}
	if card, data, ok := p.labelledCard(ctx, householdID, update, cards, text); ok {
		return p.cardStepResult(result, update, card, sourceLabelText, data), nil
	}
	names, err := p.categoryNames(ctx, householdID)
	if err != nil {
		return result, err
	}
	accounts, err := p.waitingAccounts(ctx, householdID, cards)
	if err != nil {
		return result, err
	}
	matched, answer := matchTypedCardAnswer(text, cards, names, accounts, p.turnTime(update))
	switch {
	case len(matched) == 1:
		return p.cardStepResult(result, update, matched[0], sourcePlain, ""), nil
	case len(matched) > 1:
		result.Kind, result.Candidates, result.Answer = interactionAmbiguous, matched, answer
		return result, nil
	}
	if override == interactionParserAnswer {
		if single, ok := singleTypedCard(cards); ok {
			return p.cardStepResult(result, update, single, sourcePlain, ""), nil
		}
		return result, nil
	}
	if single, ok := singleTypedCard(cards); ok && containsMoneyAmount(text, p.turnTime(update)) && expectedCardAnswer(single) != cardAnswerAmount {
		result.Kind, result.Card = interactionAnswerOrNew, interactionCard{ReviewRequestID: single.ReviewRequestID, ReviewItemID: single.ReviewItemID}
	}
	return result, nil
}

func (p *Processor) plainAnswerBlocked(ctx context.Context, householdID string, update telegramUpdate) (bool, error) {
	action, err := p.loadAgentPendingAction(ctx, householdID, update)
	if err != nil || action != nil {
		return action != nil, err
	}
	batch, err := p.loadAgentPendingBatch(ctx, householdID, update)
	if err != nil || batch != nil {
		return batch != nil, err
	}
	return p.hasPendingSalaryChoice(ctx, householdID, update)
}

func (p *Processor) rememberCard(ctx context.Context, householdID string, chatID int64) (interactionCard, bool, error) {
	rows, err := p.pool.Query(ctx, `SELECT r.id::text,ri.id::text,ri.review_type,rr.telegram_message_id,COALESCE(rr.delivered_text,'')
		FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN review_conversation c ON c.review_request_id=r.id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		WHERE r.household_id=$1 AND rr.telegram_chat_id=$2 AND rr.telegram_message_id IS NOT NULL
		  AND c.state='AWAITING_MERCHANT_DECISION' AND ri.review_type IN ('UNKNOWN_MERCHANT','AMBIGUOUS_CATEGORY')
		  AND c.updated_at>now()-interval '7 days'
		LIMIT 2`, householdID, chatID)
	if err != nil {
		return interactionCard{}, false, err
	}
	defer rows.Close()
	var found []interactionCard
	for rows.Next() {
		card := interactionCard{ChatID: chatID, State: "AWAITING_MERCHANT_DECISION"}
		if err := rows.Scan(&card.ReviewRequestID, &card.ReviewItemID, &card.ReviewType, &card.MessageID, &card.DeliveredText); err != nil {
			return interactionCard{}, false, err
		}
		found = append(found, card)
	}
	if err := rows.Err(); err != nil || len(found) != 1 {
		return interactionCard{}, false, err
	}
	return found[0], true, nil
}

func (p *Processor) cardStepResult(result interactionResult, update telegramUpdate, card agentReviewBinding, source interactionSource, data string) interactionResult {
	step := interactionCard{ReviewRequestID: card.ReviewRequestID, ReviewItemID: card.ReviewItemID, ReviewType: card.ReviewType,
		State: card.ConversationState, ChatID: update.Message.Chat.ID, MessageID: card.TelegramMessageID}
	if card.Kind != cardKindProposal {
		copied := card
		step.Binding = &copied
	} else {
		update.Message.ReplyToMessage = replyToMessage(card.TelegramMessageID)
	}
	result.Kind, result.Source, result.Card, result.Update, result.ButtonData = interactionCardStep, source, step, update, data
	return result
}

func yesNoReshowCard(cards []agentReviewBinding) (agentReviewBinding, bool) {
	if len(cards) != 1 {
		return agentReviewBinding{}, false
	}
	switch step, ok := stepForBinding(cards[0]); {
	case !ok:
		return agentReviewBinding{}, false
	case step.Fact == factSalary, step.Fact == factDate, cycleFact(step.Fact),
		step.Fact == factAmount, step.Fact == factBankAccount, step.Fact == factBankConfirm, step.Fact == factProposalIncome, step.Fact == factPurpose:
		return cards[0], true
	}
	return agentReviewBinding{}, false
}

func singleTypedCard(cards []agentReviewBinding) (agentReviewBinding, bool) {
	var found []agentReviewBinding
	for _, card := range cards {
		if expectedCardAnswer(card) != cardAnswerNone {
			found = append(found, card)
		}
	}
	if len(found) != 1 {
		return agentReviewBinding{}, false
	}
	return found[0], true
}

func containsMoneyAmount(text string, now time.Time) bool {
	if _, isDate := parseDateValue(text, now, 0); isDate {
		return false
	}
	fields := strings.Fields(strings.ToLower(text))
	for index, field := range fields {
		token := strings.Trim(field, ".,!?")
		if !amountToken.MatchString(token) {
			continue
		}
		unit := ""
		if index+1 < len(fields) {
			unit = amountUnits[strings.Trim(fields[index+1], ".,!?")]
		}
		if _, ok := parseAmountToken(token, unit); ok {
			return true
		}
	}
	return false
}

func normalizedLabel(value string) string { return normalizeAnswer(value) }

func (p *Processor) typedButtonLabel(ctx context.Context, householdID string, update telegramUpdate, card interactionCard) (string, bool) {
	text := strings.TrimSpace(update.Message.Text)
	if text == "" || len([]rune(text)) > maxLabelTextRunes || strings.ContainsAny(text, "\n") {
		return "", false
	}
	buttons, err := p.cardButtons(ctx, householdID, card.ReviewRequestID, update.Message.From.ID)
	if err != nil {
		slog.Default().Warn("load card buttons failed", "household_id", householdID, "review_request_id", card.ReviewRequestID, "error", err)
		return "", false
	}
	want := normalizedLabel(text)
	for _, button := range buttons {
		if normalizedLabel(button.Text) == want && reviewdomain.ValidTelegramCallback(button.CallbackData) {
			return button.CallbackData, true
		}
	}
	return "", false
}

func (p *Processor) labelledCard(ctx context.Context, householdID string, update telegramUpdate, cards []agentReviewBinding, text string) (agentReviewBinding, string, bool) {
	if len([]rune(text)) > maxLabelTextRunes || strings.ContainsAny(text, "\n") {
		return agentReviewBinding{}, "", false
	}
	var found agentReviewBinding
	var data string
	count := 0
	for _, card := range cards {
		probe := interactionCard{ReviewRequestID: card.ReviewRequestID}
		if callback, ok := p.typedButtonLabel(ctx, householdID, update, probe); ok {
			found, data = card, callback
			count++
		}
	}
	return found, data, count == 1
}

func (p *Processor) cardButtons(ctx context.Context, householdID, reviewRequestID string, telegramUserID int64) ([]InlineKeyboardButton, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var reviewType, state string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(ri.decision,'{}'::jsonb),ri.review_type,COALESCE(c.state,'') FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		WHERE r.id=$1 AND r.household_id=$2`, reviewRequestID, householdID).Scan(&raw, &reviewType, &state); err != nil {
		return nil, err
	}
	if state == "AWAITING_MERCHANT_DECISION" && engineReviewType(reviewType) {
		return rememberStep("", "", "").buttons(), nil
	}
	if engineOwnedType(reviewType) {
		review, err := loadEngineReview(ctx, tx, householdID, reviewRequestID, telegramUserID, false)
		if err == nil {
			review.ReadOnly = true
			if step, owned, err := p.currentStep(ctx, tx, householdID, review); err != nil || owned {
				return step.buttons(), err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	var decision reviewdec.Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return nil, err
	}
	subject, err := loadReviewSubject(ctx, tx, reviewRequestID)
	if err != nil {
		return nil, err
	}
	card := buildReviewCard(decision, reviewType, "", subject)
	markup, err := reviewCardMarkup(ctx, tx, reviewRequestID, reviewType, decision, card, subject)
	if err != nil || markup == nil {
		return nil, err
	}
	var buttons []InlineKeyboardButton
	for _, row := range markup.InlineKeyboard {
		buttons = append(buttons, row...)
	}
	return buttons, nil
}

func accountListKey(card agentReviewBinding) string {
	if card.Kind == "CYCLE_RESIDUAL" {
		return "CYCLE:" + card.ReviewRequestID
	}
	return card.ConversationState
}

func (p *Processor) waitingAccounts(ctx context.Context, householdID string, cards []agentReviewBinding) (map[string][]accountChoice, error) {
	facts := map[string]factKind{"AWAITING_ASSET_WEALTH": factAsset, "AWAITING_INVESTMENT_ACCOUNT": factInvest}
	out := map[string][]accountChoice{}
	var tx pgx.Tx
	for _, card := range cards {
		key := accountListKey(card)
		step, cycle := stepForBinding(card)
		cycle = cycle && card.Kind == "CYCLE_RESIDUAL" && (step.Fact == factCycle || step.Fact == factSplitAcct)
		fact, waiting := facts[card.ConversationState]
		if (!waiting && !cycle) || out[key] != nil {
			continue
		}
		if tx == nil {
			var err error
			if tx, err = p.pool.Begin(ctx); err != nil {
				return nil, err
			}
			defer tx.Rollback(ctx)
		}
		var choices []accountChoice
		var err error
		if cycle {
			choices, err = p.cycleCandidatesFor(ctx, tx, householdID, card.ReviewRequestID, step.Fact)
		} else {
			choices, err = accountCandidates(ctx, tx, householdID, fact)
		}
		if err != nil {
			return nil, err
		}
		out[key] = append([]accountChoice{}, choices...)
	}
	return out, nil
}
