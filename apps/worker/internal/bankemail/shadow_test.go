package bankemail

import (
	"testing"
	"time"
)

func TestCompareShadowRecordsAllRolloutFields(t *testing.T) {
	at := time.Date(2026, 8, 27, 12, 7, 53, 0, time.FixedZone("WIB", 7*60*60))
	outgoing, card, expense := "OUTGOING", "DEBIT_CARD", "EXPENSE"
	fields, agreement := CompareShadow(Extraction{AmountIDR: ptr("18090"), Direction: &outgoing, Channel: &card, TransactionAt: &at, Merchant: ptr("TOKOPEDIA")}, PolicyResult{Type: expense}, ShadowBaseline{Amount: "18090", Direction: outgoing, Channel: card, TransactionAt: at, Merchant: "TOKOPEDIA", Policy: expense})
	if !agreement || len(fields) != 6 {
		t.Fatalf("agreement=%v fields=%v", agreement, fields)
	}
	fields, agreement = CompareShadow(Extraction{AmountIDR: ptr("18091"), Direction: &outgoing, Channel: &card, TransactionAt: &at, Merchant: ptr("TOKOPEDIA")}, PolicyResult{Type: expense}, ShadowBaseline{Amount: "18090", Direction: outgoing, Channel: card, TransactionAt: at, Merchant: "TOKOPEDIA", Policy: expense})
	if agreement || fields["amount"] {
		t.Fatalf("amount mismatch not detected: agreement=%v fields=%v", agreement, fields)
	}
}
