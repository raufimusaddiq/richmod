package telegram

import (
	"context"
	"strings"
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
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Aksi ini sudah selesai atau tidak lagi tersedia.")
	}
	return true, nil
}
