package telegram

import (
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// Telegram must reuse the shared analytical READ catalog over the same fact
// engine, expose no analytical mutation tool, and validate the same arguments.
func TestTelegramExposesSharedAnalyticalReadTools(t *testing.T) {
	catalog := map[string]bool{}
	for _, tool := range AgentFinanceTools(nil, false, false, false, "", false, false, "") {
		catalog[tool.Name] = true
	}
	for _, tool := range analyticscore.Tools() {
		if class, known := agentToolClassFor(tool.Name); !known || class != agentToolRead {
			t.Fatalf("tool classification %s = %v %v", tool.Name, class, known)
		}
		if !catalog[tool.Name] {
			t.Fatalf("shared tool %s missing from the Telegram catalog", tool.Name)
		}
		raw := `{"cycle_start":"2026-08-01"}`
		if _, ok := tool.Parameters["properties"].(map[string]any)["category_ref"]; ok {
			raw = `{"cycle_start":"2026-08-01","category_ref":"category.1"}`
		}
		if _, err := ValidateNativeToolCall(gateway.ToolCall{Name: tool.Name, Arguments: json.RawMessage(raw)}); err != nil {
			t.Fatalf("validation %s: %v", tool.Name, err)
		}
	}
	if _, err := ValidateNativeToolCall(gateway.ToolCall{Name: "get_cycle_overview", Arguments: json.RawMessage(`{"cycle_start":"2026-08-01","household_id":"other"}`)}); err == nil {
		t.Fatal("unknown analytical argument accepted")
	}
	if _, err := ValidateNativeToolCall(gateway.ToolCall{Name: "execute_sql", Arguments: json.RawMessage(`{"cycle_start":null}`)}); err == nil {
		t.Fatal("unexposed analytical tool accepted")
	}
	if analyticscore.IsRead("record_transaction") {
		t.Fatal("mutation reached the analytical registry")
	}
}
