package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// IR-02: Telegram must request exactly the stored residual fact and must not
// treat an internal received-at timestamp as the supplied transaction date.
func TestResidualConfirmationBlockersRequestOnlyMissingFacts(t *testing.T) {
	decision := []byte(`{"missingFacts":["category","transaction_at"]}`)
	if got := residualConfirmationBlockers(decision, false, false, false); len(got) != 2 || got[0] != "category" || got[1] != "transaction_at" {
		t.Fatalf("blockers=%v", got)
	}
	if got := residualConfirmationBlockers(decision, true, false, false); len(got) != 1 || got[0] != "category" {
		t.Fatalf("blockers=%v; a supplied date resolves only that fact", got)
	}
	if got := residualConfirmationBlockers(decision, true, true, false); len(got) != 0 {
		t.Fatalf("all supplied facts, blockers=%v", got)
	}
	if got := residualConfirmationBlockers([]byte(`{}`), false, false, false); len(got) != 0 {
		t.Fatalf("a contract-less legacy review keeps its previous behavior, blockers=%v", got)
	}
}

func TestParseSuppliedReviewDateRejectsUnobservedFormats(t *testing.T) {
	if parsed, err := parseSuppliedReviewDate("2026-09-24"); err != nil || parsed == nil || *parsed != "2026-09-24" {
		t.Fatalf("valid date rejected: %v %v", parsed, err)
	}
	if parsed, err := parseSuppliedReviewDate(""); err != nil || parsed != nil {
		t.Fatalf("empty date is not a supplied fact: %v %v", parsed, err)
	}
	if _, err := parseSuppliedReviewDate("24/09/2026"); err == nil {
		t.Fatal("non-canonical date must not satisfy the residual contract")
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

func TestReviewDetailMarkupAllowsCategoryWithoutMerchant(t *testing.T) {
	found := false
	for _, row := range reviewDetailMarkup().InlineKeyboard {
		for _, button := range row {
			found = found || button.CallbackData == "review:category"
		}
	}
	if !found {
		t.Fatal("category must remain available when merchant is unknown")
	}
}

func TestReviewRequiresFactFailsClosedForMissingOrInvalidContract(t *testing.T) {
	for _, raw := range []*string{nil, ptr("null"), ptr("not-json"), ptr("{}")} {
		if !reviewRequiresFact(raw, "merchant") {
			t.Fatalf("reviewRequiresFact(%v) = false; malformed or absent contracts must preserve legacy requirement", raw)
		}
	}
	if reviewRequiresFact(ptr(`["category"]`), "merchant") {
		t.Fatal("category-only decision unexpectedly requires merchant")
	}
}

func ptr(value string) *string { return &value }

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
	if got := r.label(); got != "01 Agu 2026–31 Agu 2026" {
		t.Fatalf("label = %q", got)
	}
}

func TestFormatIDRSupportsNegativeCashflow(t *testing.T) {
	if got := FormatIDR("-125000"); got != "-125.000" {
		t.Fatalf("formatted=%q", got)
	}
}

func TestTransferReviewCallbacksMapDirectlyToCanonicalActions(t *testing.T) {
	tests := map[string]string{"review:own": "OWN_ACCOUNT", "review:household": "HOUSEHOLD_ACCOUNT", "review:investment": "INVESTMENT_ACCOUNT", "review:ignore": "IGNORE", "review:expense": "EXPENSE", "review:asset": "ASSET_PURCHASE", "review:unknown": ""}
	for callback, want := range tests {
		if got := transferReviewCallbackAction(callback); got != want {
			t.Fatalf("%q = %q, want %q", callback, got, want)
		}
	}
}

func TestPayslipReviewSchemaAcceptsCanonicalPayDateWithoutCategoryOptions(t *testing.T) {
	schema := reviewSchema(nil, false)
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["pay_date"]; !ok {
		t.Fatal("pay_date missing from schema")
	}
	if _, ok := properties["category_slug"]; ok {
		t.Fatal("empty category enum must be omitted")
	}
	required := schema["required"].([]string)
	if !contains(required, "pay_date") {
		t.Fatalf("required=%v", required)
	}
}
