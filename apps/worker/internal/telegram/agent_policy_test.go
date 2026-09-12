package telegram

import (
	"encoding/json"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestValidateAgentCallSetAllowsParallelReads(t *testing.T) {
	calls := []gateway.ToolCall{
		{Name: "query_spending", Arguments: json.RawMessage(`{"period":"THIS_MONTH","from_date":null,"to_date":null}`)},
		{Name: "get_category_breakdown", Arguments: json.RawMessage(`{"period":"THIS_MONTH","from_date":null,"to_date":null}`)},
	}
	plan, err := validateAgentCallSet(calls, &agentState{}, defaultAgentLimits)
	if err != nil {
		t.Fatalf("validateAgentCallSet() error = %v", err)
	}
	if plan.Class != agentToolRead || len(plan.Calls) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestValidateAgentCallSetRejectsReadMixedWithSideEffect(t *testing.T) {
	calls := []gateway.ToolCall{
		{Name: "query_spending", Arguments: json.RawMessage(`{"period":"THIS_MONTH","from_date":null,"to_date":null}`)},
		{Name: "confirm_pending_action", Arguments: json.RawMessage(`{}`)},
	}
	if _, err := validateAgentCallSet(calls, &agentState{}, defaultAgentLimits); err == nil {
		t.Fatal("mixed read + side effect should fail before execution")
	}
}

func TestValidateAgentCallSetRejectsMultipleSideEffects(t *testing.T) {
	calls := []gateway.ToolCall{
		{Name: "confirm_pending_action", Arguments: json.RawMessage(`{}`)},
		{Name: "cancel_pending_action", Arguments: json.RawMessage(`{}`)},
	}
	if _, err := validateAgentCallSet(calls, &agentState{}, defaultAgentLimits); err == nil {
		t.Fatal("multiple side effects should fail before execution")
	}
}

func TestValidateAgentCallSetRejectsSecondSideEffectInTurn(t *testing.T) {
	calls := []gateway.ToolCall{{Name: "confirm_pending_action", Arguments: json.RawMessage(`{}`)}}
	state := &agentState{SideEffects: 1}
	if _, err := validateAgentCallSet(calls, state, defaultAgentLimits); err == nil {
		t.Fatal("second side effect should be rejected")
	}
}

func TestValidateAgentCallSetEnforcesReadBudget(t *testing.T) {
	calls := []gateway.ToolCall{{Name: "query_spending", Arguments: json.RawMessage(`{"period":"THIS_MONTH","from_date":null,"to_date":null}`)}}
	state := &agentState{ReadCalls: defaultAgentLimits.MaxReadCallsPerTurn}
	if _, err := validateAgentCallSet(calls, state, defaultAgentLimits); err == nil {
		t.Fatal("read budget should be enforced")
	}
}

func TestAgentFinanceToolsUsesConversationTextInsteadOfArtificialResponseTools(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, false, false, false, "", false, false, "")
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	if seen["ask_clarification"] || seen["finance_help"] || seen["finance_out_of_scope"] {
		t.Fatalf("artificial conversation tools leaked into agent catalog: %#v", seen)
	}
	if !seen["query_spending"] || !seen["get_category_breakdown"] || !seen["record_transaction"] {
		t.Fatalf("required finance capabilities missing: %#v", seen)
	}
}
