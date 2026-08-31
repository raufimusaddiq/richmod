package bankemail

import (
	"testing"
	"time"
)

func TestEvaluateBankEmailPolicy(t *testing.T) {
	listener := Listener{TrackingPolicy: "SPENDING_ONLY"}
	incoming := "INCOMING"
	result := EvaluateBankEmail(listener, Extraction{Kind: "TRANSACTION", Direction: &incoming, AmountIDR: ptr("1"), TransactionAt: timePtr()}, nil)
	if result.Type != "IGNORE" {
		t.Fatalf("incoming=%+v", result)
	}
	outgoing := "OUTGOING"
	channel := "QR"
	result = EvaluateBankEmail(listener, Extraction{Kind: "TRANSACTION", Direction: &outgoing, Channel: &channel, AmountIDR: ptr("1"), TransactionAt: timePtr()}, nil)
	if result.Type != "EXPENSE" || result.Status != "NEEDS_REVIEW" {
		t.Fatalf("expense=%+v", result)
	}
}

func TestApplyEmailReceivedTimeFallback(t *testing.T) {
	receivedAt := time.Date(2026, 8, 31, 3, 32, 46, 0, time.FixedZone("UTC+7", 7*60*60))
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("13120"), MissingFields: []string{"transaction_at", "merchant"}, Review: &ReviewSuggestion{Summary: "Waktu transaksi tidak tertulis.", MissingFields: []string{"transaction_at", "merchant"}, SuggestedActions: []string{"USE_EMAIL_RECEIVED_AT", "ENTER_TRANSACTION_TIME", "IGNORE"}}}
	if !applyEmailReceivedTimeFallback(&extraction, receivedAt) {
		t.Fatal("expected received email time fallback")
	}
	if extraction.TransactionAt == nil || !extraction.TransactionAt.Equal(receivedAt) || extraction.TransactionAtSource != "EMAIL_RECEIVED_AT" {
		t.Fatalf("fallback=%+v", extraction)
	}
	if missing(extraction, "transaction_at") || !missing(extraction, "merchant") {
		t.Fatalf("missing fields=%v", extraction.MissingFields)
	}
	if extraction.Review == nil || len(extraction.Review.MissingFields) != 1 || extraction.Review.MissingFields[0] != "merchant" {
		t.Fatalf("review missing fields=%+v", extraction.Review)
	}
	if len(extraction.Review.SuggestedActions) != 1 || extraction.Review.SuggestedActions[0] != "IGNORE" {
		t.Fatalf("review actions=%v", extraction.Review.SuggestedActions)
	}
	if extraction.Review.Summary != "Waktu transaksi menggunakan waktu penerimaan email. Lengkapi detail transaksi yang masih diperlukan." {
		t.Fatalf("review summary=%q", extraction.Review.Summary)
	}
	if applyEmailReceivedTimeFallback(&extraction, receivedAt) {
		t.Fatal("existing transaction time must not be replaced")
	}
	if applyEmailReceivedTimeFallback(&Extraction{Kind: "NON_TRANSACTION", AmountIDR: ptr("1")}, receivedAt) {
		t.Fatal("non-transaction must not receive fallback")
	}
}
func ptr(v string) *string { return &v }
func timePtr() *time.Time  { v := time.Now(); return &v }
