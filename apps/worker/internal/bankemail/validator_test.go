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

func TestValidateEmitBankTransactionAcceptsBoundedReviewSuggestion(t *testing.T) {
	call := gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(`{"kind":"TRANSACTION","direction":"OUTGOING","channel":"DEBIT_CARD","amount_idr":"13120","transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":"Transaksi kartu debit","missing_fields":["transaction_at","merchant"],"confidence":0.99,"review":{"summary":"Transaksi kartu debit Rp13.120. Waktu transaksi tidak tertulis.","missingFields":["transaction_at","merchant"],"suggestedActions":["USE_EMAIL_RECEIVED_AT","ENTER_TRANSACTION_TIME","IGNORE"]}}`)}
	value, err := ValidateEmitBankTransaction(call)
	if err != nil || value.Review == nil || value.Review.Summary == "" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}

func TestValidateEmitBankTransactionRejectsUnsafeReviewSuggestion(t *testing.T) {
	call := gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(`{"kind":"TRANSACTION","direction":"OUTGOING","channel":"DEBIT_CARD","amount_idr":"13120","transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":["transaction_at"],"confidence":0.99,"review":{"summary":"Pilih tindakan","missingFields":["transaction_at"],"suggestedActions":["CONFIRM"]}}`)}
	if _, err := ValidateEmitBankTransaction(call); err == nil {
		t.Fatal("unsafe review action accepted")
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

func TestValidateEmitBankTransactionRejectsMissingRequiredFieldsAndTrailingData(t *testing.T) {
	complete := `{"kind":"NON_TRANSACTION","direction":null,"channel":null,"amount_idr":null,"transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":[],"confidence":0.8}`
	missing := `{"kind":"NON_TRANSACTION","direction":null,"channel":null,"amount_idr":null,"transaction_at":null,"merchant":null,"counterparty":null,"reference":null,"description":null,"confidence":0.8}`
	for name, raw := range map[string]string{"missing required array": missing, "malformed trailing data": complete + " trailing"} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateEmitBankTransaction(gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(raw)}); err == nil {
				t.Fatal("invalid arguments should fail closed")
			}
		})
	}
}

func TestValidateEmitBankTransactionEnumsAreCaseSensitive(t *testing.T) {
	raw := `{"kind":"TRANSACTION","direction":"outgoing","channel":"QR","amount_idr":"1","transaction_at":"2026-08-28T10:00:00+07:00","merchant":null,"counterparty":null,"reference":null,"description":null,"missing_fields":[],"confidence":0.8}`
	if _, err := ValidateEmitBankTransaction(gateway.ToolCall{Name: "emit_bank_transaction", Arguments: json.RawMessage(raw)}); err == nil {
		t.Fatal("non-canonical enum should fail closed")
	}
}
