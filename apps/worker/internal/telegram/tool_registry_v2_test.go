package telegram

import (
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestNativeV2ConditionalToolsAndCorrectionReference(t *testing.T) {
	tools := NativeFinanceTools([]string{"belanja"}, false, false, false, true, true)
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	if !seen["resolve_salary_choice"] || !seen["resolve_merchant_learning"] {
		t.Fatal("conditional native tools missing")
	}
	args, _ := json.Marshal(map[string]any{"target_ref": "tx_2", "search_text": nil, "period": "THIS_MONTH", "from_date": nil, "to_date": nil, "category_slug": nil, "description": "koreksi", "date_reference": nil, "explicit_date": nil, "local_time": nil})
	call := gateway.ToolCall{Name: "propose_transaction_correction", Arguments: args}
	if _, err := ValidateNativeToolCall(call); err != nil {
		t.Fatalf("reference correction rejected: %v", err)
	}
}
