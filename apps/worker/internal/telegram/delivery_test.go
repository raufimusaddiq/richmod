package telegram

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A Telegram rejection keeps its description and is classified: a 400/403 is
// permanent (the queue must not retry it blindly), a 429 carries retry_after,
// and a 5xx stays retryable. The bot token never appears in the error.
func TestSendClassifiesTelegramRejections(t *testing.T) {
	cases := []struct {
		status     int
		body       string
		permanent  bool
		retryAfter time.Duration
	}{
		{http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, true, 0},
		{http.StatusForbidden, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, true, 0},
		{http.StatusTooManyRequests, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 17","parameters":{"retry_after":17}}`, false, 17 * time.Second},
		{http.StatusBadGateway, `{"ok":false,"description":"Bad Gateway"}`, false, 0},
		{http.StatusUnauthorized, `{"ok":false,"description":"Unauthorized"}`, false, 0},
	}
	for _, tc := range cases {
		bot, _ := newFakeTelegram(t, func(string) (int, string) { return tc.status, tc.body })
		_, err := bot.Send(context.Background(), SendPayload{ChatID: 1, Text: "halo"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.status {
			t.Fatalf("HTTP %d: error %v is not a classified APIError", tc.status, err)
		}
		if apiErr.Permanent() != tc.permanent || apiErr.RetryAfter() != tc.retryAfter {
			t.Fatalf("HTTP %d: permanent=%v retryAfter=%v", tc.status, apiErr.Permanent(), apiErr.RetryAfter())
		}
		if strings.Contains(err.Error(), "token") || apiErr.Description == "" {
			t.Fatalf("HTTP %d: error %q leaks the token or lost the description", tc.status, err)
		}
	}
}

func TestEditOutcomeClassification(t *testing.T) {
	notModified := &APIError{StatusCode: 400, Description: "Bad Request: message is not modified: specified new message content and reply markup are exactly the same"}
	if !editNotModified(notModified) || editUnreachable(notModified) {
		t.Fatal("not modified must be a success, not an unreachable chat")
	}
	if !editUnreachable(&APIError{StatusCode: 403, Description: "Forbidden: bot was blocked by the user"}) {
		t.Fatal("a blocked bot is unreachable")
	}
	cannotEdit := &APIError{StatusCode: 400, Description: "Bad Request: message can't be edited"}
	if editNotModified(cannotEdit) || editUnreachable(cannotEdit) || !editRejected(cannotEdit) {
		t.Fatal("an uneditable message must be repaired by a bound resend")
	}
	if editRejected(&APIError{StatusCode: 429}) || editRejected(errors.New("edit Telegram message failed")) {
		t.Fatal("rate limits and transport failures are retried")
	}
}

func TestDescribeReviewOutcome(t *testing.T) {
	cases := []struct{ status, action, resolver, category, transaction, want string }{
		{"OPEN", "", "", "", "NEEDS_REVIEW", ""},
		{"RESOLVED", "TELEGRAM_CONFIRM", "Rina", "Makan", "CONFIRMED", "dicatat sebagai Makan oleh Rina"},
		{"RESOLVED", "IGNORE", "Budi", "", "IGNORED", "diabaikan oleh Budi"},
		{"RESOLVED", "NO_LONGER_APPLICABLE", "Budi", "", "CONFIRMED", "ditutup otomatis karena tidak lagi berlaku"},
		{"RESOLVED", "SET_WEALTH_ACCOUNT", "", "", "", "diselesaikan"},
		{"CANCELLED", "", "", "", "", "ditutup"},
	}
	for _, tc := range cases {
		if got := describeReviewOutcome(tc.status, tc.action, tc.resolver, tc.category, tc.transaction); got != tc.want {
			t.Fatalf("%+v: got %q", tc, got)
		}
	}
	if note := reviewClosureNote("RESOLVED", "dicatat sebagai Makan oleh Rina"); note != "✅ Tinjauan ini sudah selesai: dicatat sebagai Makan oleh Rina." {
		t.Fatalf("resolved note = %q", note)
	}
	if note := reviewClosureNote("EXPIRED", ""); !strings.Contains(note, "Kotak Tinjauan") {
		t.Fatalf("expired note must say the review is still in the Review Inbox: %q", note)
	}
}
