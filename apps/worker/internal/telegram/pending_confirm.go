package telegram

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	callbackPendingActionYes = "pending:action:yes"
	callbackPendingActionNo  = "pending:action:no"
	callbackPendingBatchYes  = "pending:batch:yes"
	callbackPendingBatchNo   = "pending:batch:no"
)

func pendingActionMarkup(token string) *InlineKeyboardMarkup {
	if uuid.Validate(token) != nil {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ya, simpan perubahan", CallbackData: callbackPendingActionYes + ":" + token}, {Text: "Batal", CallbackData: callbackPendingActionNo + ":" + token}}}}
}

func pendingBatchMarkup(token string) *InlineKeyboardMarkup {
	if uuid.Validate(token) != nil {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ya, catat semua", CallbackData: callbackPendingBatchYes + ":" + token}, {Text: "Batal", CallbackData: callbackPendingBatchNo + ":" + token}}}}
}

func agentPendingMarkup(history []agentToolResult) *InlineKeyboardMarkup {
	for index := len(history) - 1; index >= 0; index-- {
		result := history[index]
		if result.Class != agentToolSideEffect {
			continue
		}
		action, _ := result.Mutation["action"].(string)
		switch {
		case result.Status == "PENDING_CONFIRMATION" && action == "BATCH_STAGED", result.Status == "UPDATED" && action == "BATCH_UPDATED":
			return pendingBatchMarkup(result.confirmationToken)
		case result.Status == "PENDING_CONFIRMATION" && action == "CORRECTION_STAGED", result.Status == "EDIT_CONFIRMATION_REQUIRED":
			return pendingActionMarkup(result.confirmationToken)
		}
		return nil
	}
	return nil
}

func (p *Processor) processPendingConfirmCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) (bool, error) {
	if !strings.HasPrefix(data, "pending:") {
		return false, nil
	}
	var handled bool
	var err error
	choice, token := "", ""
	if index := strings.LastIndex(data, ":"); index >= 0 {
		choice, token = data[:index], data[index+1:]
	}
	if uuid.Validate(token) != nil {
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, staleActionMessage)
	}
	switch choice {
	case callbackPendingActionYes:
		handled, err = p.processPendingEdit(ctx, householdID, update, sourceEventID, true, token)
	case callbackPendingActionNo:
		handled, err = p.processPendingEdit(ctx, householdID, update, sourceEventID, false, token)
	case callbackPendingBatchYes:
		handled, err = p.processPendingBatch(ctx, householdID, update, sourceEventID, true, token)
	case callbackPendingBatchNo:
		handled, err = p.processPendingBatch(ctx, householdID, update, sourceEventID, false, token)
	}
	if err != nil {
		return true, err
	}
	if !handled {
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, staleActionMessage)
	}
	outcome := "Dikonfirmasi."
	if strings.HasSuffix(choice, ":no") {
		outcome = "Dibatalkan."
	}
	_ = enqueueRetireButtons(ctx, p.pool, update, outcome)
	return true, nil
}

const staleActionMessage = "Aksi ini sudah selesai atau tidak lagi tersedia."

type jobExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func retiredMessageText(original, outcome string) string {
	original = strings.TrimSpace(original)
	if original == "" {
		return ""
	}
	suffix := "\n\n" + outcome
	if room := 4000 - len([]rune(suffix)); len([]rune(original)) > room {
		original = string([]rune(original)[:room-1]) + "…"
	}
	return original + suffix
}

func enqueueRetireButtons(ctx context.Context, db jobExecer, update telegramUpdate, outcome string) error {
	if update.CallbackQuery == nil {
		return nil
	}
	text := retiredMessageText(update.CallbackQuery.Message.Text, outcome)
	if text == "" || update.CallbackQuery.Message.MessageID == 0 {
		return nil
	}
	encoded, err := json.Marshal(&InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}})
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO job(type,lane,payload_json)
		SELECT 'EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_strip_nulls(jsonb_build_object('chat_id',$1::bigint,'message_id',$2::bigint,'text',$3::text,'reply_markup',$4::jsonb,'outcome_request_id',card.request_id,'outcome_revision',CASE WHEN card.request_id IS NULL THEN NULL ELSE 0 END))
		FROM (SELECT (SELECT c.review_request_id::text FROM review_request_recipient rr JOIN review_conversation c ON c.review_request_id=rr.review_request_id
			WHERE rr.telegram_chat_id=$1 AND (rr.telegram_message_id=$2 OR rr.merchant_learning_message_id=$2) ORDER BY c.updated_at DESC LIMIT 1) AS request_id) card
		WHERE NOT EXISTS(SELECT 1 FROM review_request_recipient rr JOIN review_conversation c ON c.review_request_id=rr.review_request_id
			WHERE rr.telegram_chat_id=$1 AND (rr.telegram_message_id=$2 OR rr.merchant_learning_message_id=$2) AND COALESCE(c.context_json->'card_outcomes','{}'::jsonb) ? ($1::text||':'||$2::text))`, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, text, string(encoded))
	return err
}
