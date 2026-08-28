package bankemail

import (
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

const ToolSchemaVersion = "emit_bank_transaction.v1"

type Listener struct {
	ID, HouseholdID, BankName, SenderAddress, AccountID string
	TrackingPolicy                                      string
	Active                                              bool
}

type TrustedEmail struct {
	MessageID, Subject, Date, AuthenticationResults, Body string
}

type Extraction struct {
	Kind          string     `json:"kind"`
	Direction     *string    `json:"direction"`
	Channel       *string    `json:"channel"`
	AmountIDR     *string    `json:"amount_idr"`
	TransactionAt *time.Time `json:"transaction_at"`
	Merchant      *string    `json:"merchant"`
	Counterparty  *string    `json:"counterparty"`
	Reference     *string    `json:"reference"`
	Description   *string    `json:"description"`
	MissingFields []string   `json:"missing_fields"`
	Confidence    float64    `json:"confidence"`
}

type KnownAccount struct{ MatchHint, Relationship string }

func EmitBankTransactionTool() gateway.ToolDefinition {
	return gateway.ToolDefinition{Name: "emit_bank_transaction", Description: "Extract observed facts from one trusted bank notification email.", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "properties": map[string]any{
			"kind":       map[string]any{"type": "string", "enum": []string{"TRANSACTION", "NON_TRANSACTION", "UNKNOWN"}},
			"direction":  nullableEnum([]string{"OUTGOING", "INCOMING", "INTERNAL", "UNKNOWN"}),
			"channel":    nullableEnum([]string{"DEBIT_CARD", "MERCHANT_PAYMENT", "QR", "TRANSFER", "ATM", "BANK_FEE", "INTERNAL_TRANSFER", "RDN", "OTHER", "UNKNOWN"}),
			"amount_idr": map[string]any{"type": []string{"string", "null"}}, "transaction_at": map[string]any{"type": []string{"string", "null"}},
			"merchant": map[string]any{"type": []string{"string", "null"}}, "counterparty": map[string]any{"type": []string{"string", "null"}},
			"reference": map[string]any{"type": []string{"string", "null"}}, "description": map[string]any{"type": []string{"string", "null"}},
			"missing_fields": map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{"type": "string", "enum": []string{"amount_idr", "transaction_at", "merchant", "counterparty", "reference", "description", "direction", "channel"}}},
			"confidence":     map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		}, "required": []string{"kind", "direction", "channel", "amount_idr", "transaction_at", "merchant", "counterparty", "reference", "description", "missing_fields", "confidence"},
	}}
}

func nullableEnum(values []string) map[string]any {
	items := make([]any, 0, len(values)+1)
	for _, v := range values {
		items = append(items, v)
	}
	items = append(items, nil)
	return map[string]any{"type": []string{"string", "null"}, "enum": items}
}
