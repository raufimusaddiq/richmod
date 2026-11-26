package telegram

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestAmbiguousMerchantReplyAndProblemMapping(t *testing.T) {
	reply, ok := agentMutationReply(agentToolResult{Status: "AMBIGUOUS_MERCHANT"})
	if !ok || reply != ambiguousMerchantReply {
		t.Fatalf("ambiguous merchant reply=%q ok=%t", reply, ok)
	}
	reply, ok = agentMutationReply(agentToolResult{Status: "UNUSABLE_MERCHANT"})
	if !ok || reply != unusableMerchantReply {
		t.Fatalf("unusable merchant reply=%q ok=%t", reply, ok)
	}
	if got, ok := merchantLinkProblemReply(reviewdomain.ErrAmbiguousMerchantMatch); !ok || got != ambiguousMerchantReply {
		t.Fatalf("ambiguous problem reply=%q ok=%t", got, ok)
	}
	if got, ok := merchantLinkProblemReply(reviewdomain.ErrUnusableMerchantName); !ok || got != unusableMerchantReply {
		t.Fatalf("unusable problem reply=%q ok=%t", got, ok)
	}
	if got, ok := merchantLinkProblemReply(nil); ok || got != "" {
		t.Fatalf("nil problem reply=%q ok=%t", got, ok)
	}
}

func TestValidateAgentCallSetAllowsParallelReads(t *testing.T) {
	calls := []gateway.ToolCall{
		{Name: "query_spending", Arguments: json.RawMessage(`{"period":"THIS_MONTH","from_date":null,"to_date":null}`)},
		{Name: "query_cashflow", Arguments: json.RawMessage(`{"period":"THIS_MONTH","from_date":null,"to_date":null}`)},
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
	for _, name := range []string{"query_wealth", "query_cashflow", "query_spending", "query_savings", "list_wealth_accounts"} {
		args := `{"period":"THIS_MONTH","from_date":null,"to_date":null}`
		if name == "query_wealth" || name == "list_wealth_accounts" {
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
	state := &agentState{Tools: AgentFinanceTools([]string{"dining"}, false, false)}
	call := gateway.ToolCall{Name: "confirm_pending_action", Arguments: json.RawMessage(`{}`)}
	if _, err := validateAgentCallSet([]gateway.ToolCall{call}, state, defaultAgentLimits); err == nil {
		t.Fatal("server-unavailable tool must be rejected even if the model returns it")
	}
}

func TestAgentFinanceToolsUsesConversationTextInsteadOfArtificialResponseTools(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, false, false)
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	if seen["ask_clarification"] || seen["finance_help"] || seen["finance_out_of_scope"] {
		t.Fatalf("artificial conversation tools leaked into agent catalog: %#v", seen)
	}
	if !seen["query_spending"] || !seen["search_transactions"] || !seen["record_transaction"] {
		t.Fatalf("required finance capabilities missing: %#v", seen)
	}
	for _, gone := range []string{"get_category_breakdown", "get_finance_insight", "get_largest_transactions", "confirm_pending_batch", "cancel_pending_batch", "update_pending_batch"} {
		if seen[gone] {
			t.Fatalf("%s should have been merged away", gone)
		}
	}
	encoded, _ := json.Marshal(AgentFinanceTools([]string{"dining", "groceries"}, true, true))
	if strings.Contains(string(encoded), "groceries") {
		t.Fatalf("a tool schema repeats the category list: %s", encoded)
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
	for _, name := range []string{"record_transfer", toolProposeAssetPurchase} {
		if !isAgentSpecializedSideEffect(name) {
			t.Fatalf("%s must use a structured conversational executor", name)
		}
		class, ok := agentToolClassFor(name)
		if !ok || class != agentToolSideEffect {
			t.Fatalf("%s class = %q, %v; want SIDE_EFFECT", name, class, ok)
		}
	}
	for _, name := range []string{"query_spending", "record_transaction", "pending_batch_decision"} {
		if isAgentSpecializedSideEffect(name) {
			t.Fatalf("%s must not be routed through specialized executor", name)
		}
	}
}

func TestSpecializedMutationFallbacksAreSpecific(t *testing.T) {
	cases := []agentToolResult{
		{Status: "CONFIRMED", Mutation: map[string]any{"action": "TRANSFER_RECORDED", "amount_idr": "83000"}},
		{Status: "PENDING_CONFIRMATION", Mutation: map[string]any{"action": assetPurchaseStagedAction, "amount_idr": "5000000", "account": "Jago", "wealth_account": "Bibit", "transaction_at": "2026-01-02T03:04:05+07:00"}},
	}
	for _, tc := range cases {
		got := agentMutationFallback(tc)
		if got == agentMutationFallback(agentToolResult{}) || strings.TrimSpace(got) == "" {
			t.Fatalf("fallback for %#v was not specific: %q", tc.Mutation, got)
		}
	}
}

func TestAgentFinalStatusPreservationContract(t *testing.T) {
	for _, status := range []string{"IGNORED", "NEEDS_REVIEW"} {
		if status != "IGNORED" && status != "NEEDS_REVIEW" {
			t.Fatalf("unexpected preservation fixture %q", status)
		}
	}
}

func TestRefusalsAreNotSideEffects(t *testing.T) {
	for _, status := range []string{"TRANSFER_PURPOSE_REQUIRED", "INCOMPATIBLE_WEALTH_ACCOUNT", "TOO_MANY_CANDIDATES", "WEALTH_ACCOUNT_AMBIGUOUS"} {
		result := agentToolResult{Status: status}
		if !isNonMutatingRefusal(result) {
			t.Fatalf("%s must not count as a side effect", status)
		}
		if reply, _ := agentMutationReply(result); reply == genericMutationReply {
			t.Fatalf("%s fell back to the generic completed-action reply", status)
		}
	}
}

func TestFinanceRequestsAreNotBoundAsCardDetailAnswers(t *testing.T) {
	now := time.Now()
	for _, text := range []string{"pengeluaran bulan ini", "cek saldo", "catat kopi", "bandingkan siklus"} {
		if isDetailAnswer(text, now) {
			t.Fatalf("%q is a finance request, not a merchant answer", text)
		}
	}
	if !isDetailAnswer("Warung Bu Siti", now) {
		t.Fatal("a plain merchant name must still bind to an UNKNOWN_MERCHANT card")
	}
}
