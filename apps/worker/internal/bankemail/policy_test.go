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
func ptr(v string) *string { return &v }
func timePtr() *time.Time  { v := time.Now(); return &v }
