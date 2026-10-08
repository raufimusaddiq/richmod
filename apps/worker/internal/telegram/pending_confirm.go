package telegram

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Pending corrections and pending batches are confirmed with buttons, not by
// typing "yes"/"no". The callback carries only the bounded choice; the pending
// row is still resolved by the same Go functions the typed path uses, scoped to
// the household, Telegram user, and chat, so a stale or foreign button cannot
// confirm anything.
const (
	callbackPendingActionYes = "pending:action:yes"
	callbackPendingActionNo  = "pending:action:no"
	callbackPendingBatchYes  = "pending:batch:yes"
	callbackPendingBatchNo   = "pending:batch:no"
)

func pendingActionMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ya, simpan perubahan", CallbackData: callbackPendingActionYes}, {Text: "Batal", CallbackData: callbackPendingActionNo}}}}
}

func pendingBatchMarkup() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Ya, catat semua", CallbackData: callbackPendingBatchYes}, {Text: "Batal", CallbackData: callbackPendingBatchNo}}}}
}

// agentPendingMarkup returns confirmation buttons when the turn's latest
// side effect staged something that waits for the household's answer.
func agentPendingMarkup(history []agentToolResult) *InlineKeyboardMarkup {
	for index := len(history) - 1; index >= 0; index-- {
		result := history[index]
		if result.Class != agentToolSideEffect {
			continue
		}
		action, _ := result.Mutation["action"].(string)
		switch {
		case result.Status == "PENDING_CONFIRMATION" && action == "BATCH_STAGED":
			return pendingBatchMarkup()
		case result.Status == "PENDING_CONFIRMATION" && action == "CORRECTION_STAGED", result.Status == "EDIT_CONFIRMATION_REQUIRED":
			return pendingActionMarkup()
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
	switch data {
	case callbackPendingActionYes:
		handled, err = p.processPendingEdit(ctx, householdID, update, sourceEventID, true)
	case callbackPendingActionNo:
		handled, err = p.processPendingEdit(ctx, householdID, update, sourceEventID, false)
	case callbackPendingBatchYes:
		handled, err = p.processPendingBatch(ctx, householdID, update, sourceEventID, true)
	case callbackPendingBatchNo:
		handled, err = p.processPendingBatch(ctx, householdID, update, sourceEventID, false)
	}
	if err != nil {
		return true, err
	}
	if !handled {
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, staleActionMessage)
	}
	// The answer is final, so the buttons on the question must stop working.
	// Best effort: the decision above is already committed and idempotent.
	outcome := "Dikonfirmasi."
	if strings.HasSuffix(data, ":no") {
		outcome = "Dibatalkan."
	}
	_ = enqueueRetireButtons(ctx, p.pool, update, outcome)
	return true, nil
}

// staleActionMessage answers a tap on a button whose question is already
// resolved, expired, or replaced.
const staleActionMessage = "Aksi ini sudah selesai atau tidak lagi tersedia."

type jobExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type callbackQuestionKey struct{}

// withCallbackQuestion remembers the text of the message a callback came from.
// The Telegram update types are built by hand in many tests, so the text is read
// from the raw stored payload instead of widening them.
func withCallbackQuestion(ctx context.Context, rawPayload string) context.Context {
	var payload struct {
		CallbackQuery struct {
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
		} `json:"callback_query"`
	}
	if json.Unmarshal([]byte(rawPayload), &payload) != nil || strings.TrimSpace(payload.CallbackQuery.Message.Text) == "" {
		return ctx
	}
	return context.WithValue(ctx, callbackQuestionKey{}, payload.CallbackQuery.Message.Text)
}

func callbackQuestion(ctx context.Context) string {
	text, _ := ctx.Value(callbackQuestionKey{}).(string)
	return text
}

// retiredMessageText is the original question followed by what happened to it.
func retiredMessageText(original, outcome string) string {
	original = strings.TrimSpace(original)
	if original == "" {
		return ""
	}
	// Telegram caps a message at 4096 and clean() at 4000 runes. Trim the
	// question, never the outcome, so a long question still shows what happened.
	suffix := "\n\n" + outcome
	if room := 4000 - len([]rune(suffix)); len([]rune(original)) > room {
		original = string([]rune(original)[:room-1]) + "…"
	}
	return original + suffix
}

// enqueueRetireButtons edits the message a callback came from so its buttons
// disappear and the outcome is visible, which stops a second tap from reaching
// a resolved question. Telegram can only edit text it can resend, so a message
// whose text is unknown is left alone.
func enqueueRetireButtons(ctx context.Context, db jobExecer, update telegramUpdate, outcome string) error {
	if update.CallbackQuery == nil {
		return nil
	}
	text := retiredMessageText(callbackQuestion(ctx), outcome)
	if text == "" || update.CallbackQuery.Message.MessageID == 0 {
		return nil
	}
	encoded, err := json.Marshal(&InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}})
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO job(type,lane,payload_json) VALUES('EDIT_TELEGRAM_MESSAGE','INTERACTIVE',jsonb_build_object('chat_id',$1::bigint,'message_id',$2::bigint,'text',$3::text,'reply_markup',$4::jsonb))`, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, text, string(encoded))
	return err
}
