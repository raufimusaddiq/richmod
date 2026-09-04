package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestParseReviewPayDate(t *testing.T) {
	if got := parseReviewPayDate("BENAR, gaji masuk tanggal 24 agustus 2026"); got != "2026-08-24" {
		t.Fatalf("pay date = %q", got)
	}
	if got := parseReviewPayDate("BENAR, gunakan tanggal 31 februari 2026"); got != "" {
		t.Fatalf("invalid pay date = %q", got)
	}
}

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

func TestReviewDetailMarkupHidesCategoryUntilMerchantKnown(t *testing.T) {
	for _, test := range []struct {
		merchantKnown bool
		wantCategory  bool
	}{{false, false}, {true, true}} {
		found := false
		for _, row := range reviewDetailMarkup(test.merchantKnown).InlineKeyboard {
			for _, button := range row {
				found = found || button.CallbackData == "review:category"
			}
		}
		if found != test.wantCategory {
			t.Fatalf("merchantKnown=%t categoryButton=%t", test.merchantKnown, found)
		}
	}
}

func TestReviewCategoryUsesDeterministicAllowedMatch(t *testing.T) {
	processor := &Processor{gateway: reviewTestGateway{}}
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

type reviewTestGateway struct{}

func (reviewTestGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{Name: "resolve_review", Arguments: json.RawMessage(`{"category_slug":"belanja-rumah-tangga","description":"","note":"buat belanja rumah tangga","confidence":1,"ambiguous":false}`)}, gateway.Metadata{}, nil
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
	if got := r.label(); got != "01 Aug 2026–31 Aug 2026" {
		t.Fatalf("label = %q", got)
	}
}

func TestFormatIDRSupportsNegativeCashflow(t *testing.T) {
	if got := FormatIDR("-125000"); got != "-125.000" {
		t.Fatalf("formatted=%q", got)
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

func TestTransferReviewIntentIsDeterministic(t *testing.T) {
	tests := map[string]string{"rekeningku sendiri": "OWN_ACCOUNT", "transfer ke istri": "HOUSEHOLD_ACCOUNT", "masuk RDN investasi": "INVESTMENT_ACCOUNT", "abaikan saja": "IGNORE", "buat bayar tukang renovasi": "EXPENSE", "tidak yakin": ""}
	for input, want := range tests {
		if got := transferReviewIntent(input); got != want {
			t.Fatalf("%q = %q, want %q", input, got, want)
		}
	}
}

func TestMerchantRememberIntentRequiresExplicitReply(t *testing.T) {
	tests := map[string]string{
		"ingat merchant": "REMEMBER",
		"ya ingat":       "REMEMBER",
		"tidak":          "DECLINE",
		"sekali saja":    "DECLINE",
		"oke":            "",
		"mungkin":        "",
	}
	for input, want := range tests {
		if got := merchantRememberIntent(input); got != want {
			t.Fatalf("%q = %q, want %q", input, got, want)
		}
	}
}
