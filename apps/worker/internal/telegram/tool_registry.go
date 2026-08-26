package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// NativeFinanceTools is the only tool catalog exposed to the conversational
// model. Implementations are intentionally dispatched by Go, never by the
// model or the gateway.
func NativeFinanceTools() []gateway.ToolDefinition {
	stringType := map[string]any{"type": "string"}
	return []gateway.ToolDefinition{
		{Name: "query_transactions", Description: "Read household ledger transactions for a bounded period.", Parameters: objectSchema(map[string]any{"mode": map[string]any{"type": "string", "enum": []string{"spending", "cashflow", "search", "reviews"}}, "period": stringType, "search_text": map[string]any{"type": []string{"string", "null"}}}, []string{"mode", "period", "search_text"})},
		{Name: "create_transaction", Description: "Create one validated IDR income or expense proposal.", Parameters: objectSchema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"INCOME", "EXPENSE"}}, "amount_idr": stringType, "merchant": map[string]any{"type": []string{"string", "null"}}, "transaction_at": stringType}, []string{"type", "amount_idr", "merchant", "transaction_at"})},
		{Name: "create_transaction_batch", Description: "Stage multiple validated IDR income or expense entries for one combined confirmation.", Parameters: objectSchema(map[string]any{"items": map[string]any{"type": "array", "minItems": 1, "maxItems": 10, "items": objectSchema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"INCOME", "EXPENSE"}}, "amount_idr": stringType, "merchant": map[string]any{"type": []string{"string", "null"}}, "transaction_at": stringType}, []string{"type", "amount_idr", "merchant", "transaction_at"})}}, []string{"items"})},
		{Name: "propose_transaction_edit", Description: "Propose an edit to one exact existing transaction; never mutate it.", Parameters: objectSchema(map[string]any{"transaction_id": stringType, "transaction_at": map[string]any{"type": []string{"string", "null"}}, "description": map[string]any{"type": []string{"string", "null"}}}, []string{"transaction_id", "transaction_at", "description"})},
		{Name: "confirm_edit", Description: "Confirm a previously proposed transaction edit.", Parameters: objectSchema(map[string]any{"pending_action_id": stringType}, []string{"pending_action_id"})},
		{Name: "cancel_edit", Description: "Cancel a previously proposed transaction edit.", Parameters: objectSchema(map[string]any{"pending_action_id": stringType}, []string{"pending_action_id"})},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}

func ValidateNativeToolCall(call gateway.ToolCall) (map[string]any, error) {
	allowed := map[string]bool{"query_transactions": true, "create_transaction": true, "create_transaction_batch": true, "propose_transaction_edit": true, "confirm_edit": true, "cancel_edit": true}
	if !allowed[call.Name] {
		return nil, fmt.Errorf("unregistered finance tool %q", call.Name)
	}
	var args map[string]any
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil || args == nil {
		return nil, fmt.Errorf("invalid arguments for %s", call.Name)
	}
	return args, nil
}
