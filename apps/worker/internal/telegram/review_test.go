package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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

func TestReviewCategoryUsesDeterministicAllowedMatch(t *testing.T) {
	processor := &Processor{}
	result, err := processor.extractReview(context.Background(), "source-1", "buat belanja rumah tangga", []categoryChoice{
		{ID: "category-1", Name: "Belanja Rumah Tangga", Slug: "belanja-rumah-tangga"},
		{ID: "category-2", Name: "Transportasi", Slug: "transportasi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CategorySlug != "belanja-rumah-tangga" || result.Confidence != 1 || result.Ambiguous {
		t.Fatalf("unexpected extraction: %#v", result)
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

func TestReviewQuestionUsesIndonesianIDRFormat(t *testing.T) {
	if got := ReviewQuestion("55199", "PAMELLA DUA"); !strings.Contains(got, "Rp55.199") {
		t.Fatalf("question = %q", got)
	}
}

func TestIncomeReviewIntentIsDeterministic(t *testing.T) {
	if got := incomeReviewIntent("ini transfer sendiri"); got != "REJECT" {
		t.Fatalf("expected rejection, got %q", got)
	}
	if got := incomeReviewIntent("ya, ini penghasilan"); got != "CONFIRM" {
		t.Fatalf("expected confirmation, got %q", got)
	}
	if got := incomeReviewIntent("mungkin dari teman"); got != "" {
		t.Fatalf("ambiguous reply must remain open, got %q", got)
	}
}
