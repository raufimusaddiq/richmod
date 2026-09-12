package telegram

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func sideEffectNames(tools []gateway.ToolDefinition) map[string]bool {
	out := map[string]bool{}
	for _, tool := range tools {
		if class, ok := agentToolClassFor(tool.Name); ok && class == agentToolSideEffect {
			out[tool.Name] = true
		}
	}
	return out
}

func TestExplicitReviewReplyExposesOnlyBoundReviewMutation(t *testing.T) {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 77}
	tools := AgentFinanceTools([]string{"dining"}, true, true, true, "AMBIGUOUS_CATEGORY", true, true, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}, nil)
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowExactReview {
		t.Fatalf("scope=%s", scope)
	}
	if len(writes) != 1 || !writes["resolve_review"] {
		t.Fatalf("writes=%v; want resolve_review only", writes)
	}
}

func TestExplicitMerchantReplyExposesOnlyMerchantMutation(t *testing.T) {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 88}
	tools := AgentFinanceTools([]string{"dining"}, true, true, false, "", true, true, "")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, nil, &agentMerchantLearningBinding{ReviewRequestID: "review", TransactionID: "tx", TelegramMessageID: 88})
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowExactMerchant {
		t.Fatalf("scope=%s", scope)
	}
	if len(writes) != 1 || !writes["resolve_merchant_learning"] {
		t.Fatalf("writes=%v; want resolve_merchant_learning only", writes)
	}
}

func TestStaleExplicitReplyExposesNoSideEffects(t *testing.T) {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 99}
	tools := AgentFinanceTools([]string{"dining"}, true, true, true, "AMBIGUOUS_CATEGORY", true, true, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, nil, nil)
	if scope != agentWorkflowExplicitUnbound {
		t.Fatalf("scope=%s", scope)
	}
	if writes := sideEffectNames(filtered); len(writes) != 0 {
		t.Fatalf("stale explicit reply exposed side effects: %v", writes)
	}
}

func TestCycleResidualToolUsesWealthHintsNotUUIDs(t *testing.T) {
	tools := AgentFinanceTools(nil, false, false, true, "CYCLE_RESIDUAL_ALLOCATION", false, false, "CYCLE_RESIDUAL")
	var resolve *gateway.ToolDefinition
	for i := range tools {
		if tools[i].Name == "resolve_review" {
			resolve = &tools[i]
			break
		}
	}
	if resolve == nil {
		t.Fatal("resolve_review missing for cycle residual")
	}
	properties, _ := resolve.Parameters["properties"].(map[string]any)
	allocations, _ := properties["allocations"].(map[string]any)
	items, _ := allocations["items"].(map[string]any)
	itemProperties, _ := items["properties"].(map[string]any)
	if _, ok := itemProperties["wealth_account_hint"]; !ok {
		t.Fatalf("cycle residual allocation missing wealth_account_hint: %#v", itemProperties)
	}
	if _, ok := itemProperties["wealth_account_id"]; ok {
		t.Fatalf("cycle residual exposed canonical wealth_account_id: %#v", itemProperties)
	}
}
