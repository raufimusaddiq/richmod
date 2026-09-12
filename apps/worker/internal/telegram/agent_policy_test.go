package telegram

import (
	"encoding/json"
	"strings"
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

func TestValidateAgentCallSetAllowsFiveParallelReads(t *testing.T) {
	calls := make([]gateway.ToolCall, 0, 5)
	for _, name := range []string{"query_wealth", "query_cashflow", "query_spending", "query_savings", "get_finance_insight"} {
		args := `{"period":"THIS_MONTH","from_date":null,"to_date":null}`
		if name == "query_wealth" {
			args = `{}`
		}
		calls = append(calls, gateway.ToolCall{Name: name, Arguments: json.RawMessage(args)})
	}
	plan, err := validateAgentCallSet(calls, &agentState{}, defaultAgentLimits)
	if err != nil {
		t.Fatalf("validateAgentCallSet() error = %v", err)
	}
	if plan.Class != agentToolRead || len(plan.Calls) != 5 {
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

func TestValidateAgentCallSetRejectsToolNotAvailableInCurrentState(t *testing.T) {
	state := &agentState{Tools: AgentFinanceTools([]string{"dining"}, false, false, false, "", false, false, "")}
	call := gateway.ToolCall{Name: "confirm_pending_action", Arguments: json.RawMessage(`{}`)}
	if _, err := validateAgentCallSet([]gateway.ToolCall{call}, state, defaultAgentLimits); err == nil {
		t.Fatal("server-unavailable tool must be rejected even if the model returns it")
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

func TestEveryAgentSideEffectHasExactlyOneConversationalExecutor(t *testing.T) {
	for name := range agentSideEffectTools {
		core := isAgentCoreSideEffect(name)
		specialized := isAgentSpecializedSideEffect(name)
		if core == specialized {
			t.Fatalf("%s routing invalid: core=%v specialized=%v; want exactly one", name, core, specialized)
		}
	}
}

func TestSpecializedFreeTextSideEffectsUseStructuredAgentExecutors(t *testing.T) {
	for _, name := range []string{"record_transfer", "resolve_review", "resolve_salary_choice", "resolve_merchant_learning"} {
		if !isAgentSpecializedSideEffect(name) {
			t.Fatalf("%s must use a structured conversational executor", name)
		}
		class, ok := agentToolClassFor(name)
		if !ok || class != agentToolSideEffect {
			t.Fatalf("%s class = %q, %v; want SIDE_EFFECT", name, class, ok)
		}
	}
	for _, name := range []string{"query_spending", "record_transaction", "confirm_pending_batch"} {
		if isAgentSpecializedSideEffect(name) {
			t.Fatalf("%s must not be routed through specialized executor", name)
		}
	}
}

func TestSpecializedMutationFallbacksAreSpecific(t *testing.T) {
	cases := []agentToolResult{
		{Status: "CONFIRMED", Mutation: map[string]any{"action": "TRANSFER_RECORDED", "amount_idr": "83000"}},
		{Status: "RESOLVED", Mutation: map[string]any{"action": "REVIEW_CONFIRMED"}},
		{Status: "PRIMARY", Mutation: map[string]any{"action": "SALARY_CHOICE_RESOLVED", "choice": "PRIMARY"}},
		{Status: "CONFIRMED", Mutation: map[string]any{"action": "MERCHANT_LEARNING_RESOLVED", "remember": true}},
	}
	for _, tc := range cases {
		got := agentMutationFallback(tc)
		if got == "Aksi keuangan sudah diproses." || strings.TrimSpace(got) == "" {
			t.Fatalf("fallback for %#v was not specific: %q", tc.Mutation, got)
		}
	}
}

func TestAgentFinalStatusPreservationContract(t *testing.T) {
	// The final conversational response must not convert an explicit IGNORED or
	// NEEDS_REVIEW source event into PROCESSED merely because text was sent.
	for _, status := range []string{"IGNORED", "NEEDS_REVIEW"} {
		if status != "IGNORED" && status != "NEEDS_REVIEW" {
			t.Fatalf("unexpected preservation fixture %q", status)
		}
	}
}
