package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const callbackPayloadWithText = `{"update_id":1,"callback_query":{"id":"c1","data":"pending:action:yes","from":{"id":7},"message":{"message_id":55,"text":"Saya menemukan Alfamart · Rp50.000. Ubah tanggalnya ke 04 Mei 2026 04:05?","chat":{"id":7}}}}`

func TestCallbackQuestionIsDecodedIntoTelegramUpdate(t *testing.T) {
	var update telegramUpdate
	if err := json.Unmarshal([]byte(callbackPayloadWithText), &update); err != nil {
		t.Fatal(err)
	}
	if update.CallbackQuery == nil || !strings.HasPrefix(update.CallbackQuery.Message.Text, "Saya menemukan Alfamart") {
		t.Fatalf("question text was not decoded: %#v", update.CallbackQuery)
	}
	var noText telegramUpdate
	if err := json.Unmarshal([]byte(`{"callback_query":{"message":{"message_id":9}}}`), &noText); err != nil {
		t.Fatal(err)
	}
	if noText.CallbackQuery == nil || noText.CallbackQuery.Message.Text != "" {
		t.Fatalf("a payload without text must carry nothing, got %#v", noText.CallbackQuery)
	}
}

func TestRetiredMessageKeepsTheQuestionAndAddsTheOutcome(t *testing.T) {
	got := retiredMessageText("  Catat semuanya?  ", "Dikonfirmasi.")
	if got != "Catat semuanya?\n\nDikonfirmasi." {
		t.Fatalf("unexpected text %q", got)
	}
	if retiredMessageText("   ", "Dibatalkan.") != "" {
		t.Fatal("a message with unknown text must not be edited")
	}
	long := strings.Repeat("a", 5000)
	got = retiredMessageText(long, "Dibatalkan.")
	if n := len([]rune(got)); n > 4000 {
		t.Fatalf("edited text exceeds Telegram's limit: %d", n)
	}
	if !strings.HasSuffix(got, "\n\nDibatalkan.") {
		t.Fatalf("a long question must not lose the outcome: ...%q", got[len(got)-30:])
	}
	if !strings.Contains(got, "…\n\n") {
		t.Fatal("a trimmed question should show that it was shortened")
	}
}

func TestEnqueueRetireButtonsSkipsMessagesItCannotEdit(t *testing.T) {
	if err := enqueueRetireButtons(context.Background(), nil, telegramUpdate{}, "x"); err != nil {
		t.Fatal(err)
	}
	var update telegramUpdate
	update.CallbackQuery = &telegramCallbackQuery{}
	update.CallbackQuery.Message.MessageID = 9
	if err := enqueueRetireButtons(context.Background(), nil, update, "x"); err != nil {
		t.Fatalf("unknown text must be skipped without touching the database: %v", err)
	}
}
