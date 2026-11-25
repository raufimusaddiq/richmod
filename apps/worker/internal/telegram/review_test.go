package telegram

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTelegramReplyMetadataBindsExactMessage(t *testing.T) {
	var update telegramUpdate
	err := json.Unmarshal([]byte(`{"message":{"message_id":22,"text":"belanja rumah tangga","reply_to_message":{"message_id":17},"from":{"id":719809965},"chat":{"id":719809965}}}`), &update)
	if err != nil {
		t.Fatal(err)
	}
	if update.Message.ReplyToMessage == nil || update.Message.ReplyToMessage.MessageID != 17 {
		t.Fatalf("reply binding = %#v", update.Message.ReplyToMessage)
	}
}

func TestDecodeReviewSendPayload(t *testing.T) {
	payload, err := DecodeSendPayload(json.RawMessage(`{"chat_id":719809965,"reply_to_message_id":22,"text":"review","review_request_id":"review-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if payload.ReviewRequestID != "review-1" {
		t.Fatalf("review request ID = %q", payload.ReviewRequestID)
	}
}

func TestDecodeInlineReviewPayload(t *testing.T) {
	payload, err := DecodeSendPayload(json.RawMessage(`{"chat_id":719809965,"text":"review","reply_markup":{"inline_keyboard":[[{"text":"Household","callback_data":"review:household"}]]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if payload.ReplyMarkup == nil || payload.ReplyMarkup.InlineKeyboard[0][0].CallbackData != "review:household" {
		t.Fatalf("markup=%#v", payload.ReplyMarkup)
	}
}

func TestReviewQuestionUsesIndonesianIDRFormat(t *testing.T) {
	got := ReviewQuestion("55199", "PAMELLA DUA")
	if !strings.Contains(got, "Rp55.199") || !strings.Contains(got, "Merchant: PAMELLA DUA") {
		t.Fatalf("question = %q", got)
	}
}

func TestAssistantRangeLabelUsesInclusiveJakartaDates(t *testing.T) {
	location := jakartaLocation()
	r := assistantRange{From: time.Date(2026, 8, 1, 0, 0, 0, 0, location), To: time.Date(2026, 9, 1, 0, 0, 0, 0, location)}
	if got := r.label(); got != "01 Agu 2026–31 Agu 2026" {
		t.Fatalf("label = %q", got)
	}
}

func TestFormatIDRSupportsNegativeCashflow(t *testing.T) {
	if got := FormatIDR("-125000"); got != "-125.000" {
		t.Fatalf("formatted=%q", got)
	}
}
