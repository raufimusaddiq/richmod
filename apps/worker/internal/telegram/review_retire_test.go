package telegram

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestEditFinishedAndRejectedClassifyTelegramErrors(t *testing.T) {
	cases := []struct {
		name               string
		err                error
		finished, rejected bool
	}{
		{"not modified", &APIError{StatusCode: 400, Description: "Bad Request: message is not modified: specified new message content and reply markup are exactly the same"}, true, true},
		{"message gone", &APIError{StatusCode: 400, Description: "Bad Request: message to edit not found"}, true, true},
		{"cannot edit", &APIError{StatusCode: 400, Description: "Bad Request: message can't be edited"}, true, true},
		{"blocked", &APIError{StatusCode: 403, Description: "Forbidden: bot was blocked by the user"}, true, true},
		{"bad text", &APIError{StatusCode: 400, Description: "Bad Request: can't parse entities"}, false, true},
		{"rate limited", &APIError{StatusCode: 429, Description: "Too Many Requests: retry after 3"}, false, false},
		{"server error", &APIError{StatusCode: 502}, false, false},
		{"transport", errors.New("edit Telegram message failed"), false, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := editFinished(testCase.err); got != testCase.finished {
				t.Fatalf("editFinished=%v want %v", got, testCase.finished)
			}
			if got := editRejected(testCase.err); got != testCase.rejected {
				t.Fatalf("editRejected=%v want %v", got, testCase.rejected)
			}
		})
	}
}

func TestEditReplyMarkupRemovesOnlyTheKeyboard(t *testing.T) {
	bot, fake := newFakeTelegram(t, nil)
	if err := bot.EditReplyMarkup(context.Background(), 7, 31); err != nil {
		t.Fatal(err)
	}
	calls := fake.recorded()
	if len(calls) != 1 || calls[0].Method != "editMessageReplyMarkup" {
		t.Fatalf("calls = %+v", calls)
	}
	body := calls[0].Body
	if body["chat_id"] != float64(7) || body["message_id"] != float64(31) || body["text"] != nil {
		t.Fatalf("body = %+v", body)
	}
	markup, _ := body["reply_markup"].(map[string]any)
	if keyboard, ok := markup["inline_keyboard"].([]any); !ok || len(keyboard) != 0 {
		t.Fatalf("reply_markup = %+v, want an empty inline keyboard", body["reply_markup"])
	}
}

func TestBotEditErrorKeepsTelegramDescriptionWithoutToken(t *testing.T) {
	bot, _ := newFakeTelegram(t, func(string) (int, string) {
		return http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`
	})
	bot.token = "secret-token"
	err := bot.Edit(context.Background(), EditPayload{ChatID: 7, MessageID: 31, Text: "x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || apiErr.Method != "editMessageText" {
		t.Fatalf("err = %#v", err)
	}
	if !editFinished(err) {
		t.Fatalf("not-modified edit must be finished: %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaks the bot token: %v", err)
	}
}

func TestDecodeRetireCardPayloadRequiresTheCard(t *testing.T) {
	good := `{"review_request_id":"r","recipient_id":"p","chat_id":7,"message_id":31,"status":"RESOLVED"}`
	if payload, err := DecodeRetireCardPayload([]byte(good)); err != nil || payload.MessageID != 31 || payload.Status != "RESOLVED" {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
	for _, bad := range []string{
		`{"review_request_id":"r","recipient_id":"p","chat_id":7,"status":"RESOLVED"}`,
		`{"review_request_id":"r","chat_id":7,"message_id":31}`,
		`{"review_request_id":"r","recipient_id":"p","chat_id":7,"message_id":31,"text":"x"}`,
	} {
		if _, err := DecodeRetireCardPayload([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestReviewClosureNoteOnlyForTerminalStatus(t *testing.T) {
	for _, status := range []string{"RESOLVED", "CANCELLED", "EXPIRED"} {
		if reviewClosureNote(status) == "" {
			t.Fatalf("%s card has no closure note", status)
		}
	}
	for _, status := range []string{"OPEN", "PENDING_SEND", ""} {
		if reviewClosureNote(status) != "" {
			t.Fatalf("%s card must stay live", status)
		}
	}
}
