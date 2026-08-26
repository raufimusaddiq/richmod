package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestValidateExtractionUsesWholeIDRAndJakartaYesterday(t *testing.T) {
	location := jakartaLocation()
	now := time.Date(2026, time.August, 25, 0, 15, 0, 0, location)
	amount, currency := "85000", "IDR"
	dateReference, localTime := "YESTERDAY", "13:30"
	category := "makan-di-luar"

	validated, err := validateExtraction(extraction{
		Language:           "id",
		Intent:             "ADD_EXPENSE",
		Amount:             &amount,
		Currency:           &currency,
		CategorySlug:       &category,
		DateReference:      &dateReference,
		LocalTime:          &localTime,
		Confidence:         0.95,
		CategoryConfidence: 0.92,
	}, now)
	if err != nil {
		t.Fatalf("validateExtraction() error = %v", err)
	}
	if validated.Amount != "85000" || validated.Type != "EXPENSE" {
		t.Fatalf("validated = %#v", validated)
	}
	want := time.Date(2026, time.August, 24, 13, 30, 0, 0, location)
	if !validated.TransactionAt.Equal(want) {
		t.Fatalf("transaction time = %v, want %v", validated.TransactionAt, want)
	}
}

func TestValidateExtractionRejectsNonIDRAndFractionalAmount(t *testing.T) {
	now := time.Date(2026, time.August, 25, 10, 0, 0, 0, jakartaLocation())
	tests := []struct {
		name     string
		amount   string
		currency string
	}{
		{name: "non IDR", amount: "100", currency: "USD"},
		{name: "fraction", amount: "100.50", currency: "IDR"},
		{name: "leading zero", amount: "0100", currency: "IDR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateExtraction(extraction{
				Language: "id",
				Intent:   "ADD_EXPENSE", Amount: &test.amount, Currency: &test.currency,
				Confidence: 0.9, CategoryConfidence: 0.9,
			}, now)
			if err == nil {
				t.Fatal("validateExtraction() accepted invalid money")
			}
		})
	}
}

func TestExtractionSchemaRequiresEveryFieldAndRejectsExtras(t *testing.T) {
	schema := extractionSchema()
	if schema["additionalProperties"] != false {
		t.Fatal("strict schema must reject additional properties")
	}
	properties := schema["properties"].(map[string]any)
	required := schema["required"].([]string)
	if len(required) != len(properties) {
		t.Fatalf("required fields = %d, properties = %d", len(required), len(properties))
	}
}

func TestExtractionSchemaSupportsMultipleExpenseItems(t *testing.T) {
	properties := extractionSchema()["properties"].(map[string]any)
	items := properties["items"].(map[string]any)
	if items["type"] != "array" || items["maxItems"] != 10 {
		t.Fatalf("items schema = %#v", items)
	}
	itemSchema := items["items"].(map[string]any)
	itemProperties := itemSchema["properties"].(map[string]any)
	typeSchema := itemProperties["type"].(map[string]any)
	if got := typeSchema["enum"].([]string); len(got) != 2 || got[0] != "INCOME" || got[1] != "EXPENSE" {
		t.Fatalf("batch item types = %#v", got)
	}
}

func TestValidateExtractionRejectsUnsupportedLanguage(t *testing.T) {
	amount, currency := "1000", "IDR"
	_, err := validateExtraction(extraction{Language: "fr", Intent: "ADD_EXPENSE", Amount: &amount, Currency: &currency}, time.Now().In(jakartaLocation()))
	if err == nil {
		t.Fatal("accepted unsupported language")
	}
}

func TestExtractionPromptTreatsContextAsUntrusted(t *testing.T) {
	if !strings.Contains(extractionPrompt, "untrusted data") || !strings.Contains(extractionPrompt, "bypass validation") {
		t.Fatal("prompt must retain context/input injection guardrails")
	}
}

func TestAssistantRangesUseJakartaCalendarBoundaries(t *testing.T) {
	now := time.Date(2026, time.August, 26, 14, 0, 0, 0, jakartaLocation())
	period := "THIS_WEEK"
	got, err := resolveAssistantRange(now, &period, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.From.Format(time.RFC3339) != "2026-08-24T00:00:00+07:00" || got.To.Format(time.RFC3339) != "2026-08-31T00:00:00+07:00" {
		t.Fatalf("range=%s..%s", got.From.Format(time.RFC3339), got.To.Format(time.RFC3339))
	}
}

func TestAssistantCustomRangeIsBounded(t *testing.T) {
	now := time.Date(2026, time.August, 26, 14, 0, 0, 0, jakartaLocation())
	period, from, to := "CUSTOM", "2024-01-01", "2026-08-01"
	if _, err := resolveAssistantRange(now, &period, &from, &to); err == nil {
		t.Fatal("accepted disclosure range over one year")
	}
}

func TestCallbackTextContainsNoTransactionIdentity(t *testing.T) {
	if got := callbackText("review:own"); got != "rekening sendiri" {
		t.Fatalf("callback=%q", got)
	}
	if got := callbackText("transaction:00000000-0000-0000-0000-000000000000"); got != "" {
		t.Fatalf("untrusted callback accepted: %q", got)
	}
}
