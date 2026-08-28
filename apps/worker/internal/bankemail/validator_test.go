package bankemail

import (
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestValidateEmitBankTransaction(t *testing.T) {
	call := gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(`{"kind":"TRANSACTION","direction":"OUTGOING","channel":"QR","amount_idr":"40700","transaction_at":"2026-08-28T10:00:00+07:00","merchant":null,"counterparty":null,"reference":"r","description":null,"missing_fields":["merchant","description"],"confidence":0.8}`)}
	value, err := ValidateEmitBankTransaction(call)
	if err != nil || value.AmountIDR == nil || *value.AmountIDR != "40700" || value.TransactionAt == nil {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}

func TestValidateEmitBankTransactionRejectsUnknownAndFractionalAmount(t *testing.T) {
	base := `{"kind":"TRANSACTION","direction":"OUTGOING","channel":"QR","amount_idr":"40700.5","transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":["transaction_at"],"confidence":0.8}`
	if _, err := ValidateEmitBankTransaction(gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(base)}); err == nil {
		t.Fatal("fractional amount should fail")
	}
	if _, err := ValidateEmitBankTransaction(gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(`{"kind":"TRANSACTION","extra":1}`)}); err == nil {
		t.Fatal("unknown fields should fail")
	}
}
