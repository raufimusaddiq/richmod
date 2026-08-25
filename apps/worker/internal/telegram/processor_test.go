package telegram

import (
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
				Intent: "ADD_EXPENSE", Amount: &test.amount, Currency: &test.currency,
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
