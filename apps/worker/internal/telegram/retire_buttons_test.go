package telegram

import (
	"context"
	"strings"
	"testing"
)

const callbackPayloadWithText = `{"update_id":1,"callback_query":{"id":"c1","data":"pending:action:yes","from":{"id":7},"message":{"message_id":55,"text":"Saya menemukan Alfamart · Rp50.000. Ubah tanggalnya ke 04 Mei 2026 04:05?","chat":{"id":7}}}}`

func TestCallbackQuestionIsReadFromTheStoredPayload(t *testing.T) {
	ctx := withCallbackQuestion(context.Background(), callbackPayloadWithText)
	if !strings.HasPrefix(callbackQuestion(ctx), "Saya menemukan Alfamart") {
		t.Fatalf("question text was not carried: %q", callbackQuestion(ctx))
	}
	if got := callbackQuestion(withCallbackQuestion(context.Background(), `{"callback_query":{"message":{"message_id":9}}}`)); got != "" {
		t.Fatalf("a payload without text must carry nothing, got %q", got)
	}
	if got := callbackQuestion(withCallbackQuestion(context.Background(), `not json`)); got != "" {
		t.Fatalf("a bad payload must carry nothing, got %q", got)
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
	// No callback, or a callback whose question text is unknown, must return
	// before touching the database (nil here would panic if it did).
	if err := enqueueRetireButtons(context.Background(), nil, telegramUpdate{}, "x"); err != nil {
		t.Fatal(err)
	}
	var update telegramUpdate
	update.CallbackQuery = &struct {
		ID   string `json:"id"`
		Data string `json:"data"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Message struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	}{}
	update.CallbackQuery.Message.MessageID = 9
	if err := enqueueRetireButtons(context.Background(), nil, update, "x"); err != nil {
		t.Fatalf("unknown text must be skipped without touching the database: %v", err)
	}
}
