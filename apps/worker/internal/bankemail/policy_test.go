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
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: ptr("13120"), MissingFields: []string{"transaction_at", "merchant"}}
	if !applyEmailReceivedTimeFallback(&extraction, receivedAt) {
		t.Fatal("expected received email time fallback")
	}
	if extraction.TransactionAt == nil || !extraction.TransactionAt.Equal(receivedAt) || extraction.TransactionAtSource != "EMAIL_RECEIVED_AT" {
		t.Fatalf("fallback=%+v", extraction)
	}
	if missing(extraction, "transaction_at") || !missing(extraction, "merchant") {
		t.Fatalf("missing fields=%v", extraction.MissingFields)
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
