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

func TestPendingBatchDoesNotOwnUnrelatedAgentTurn(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, false, true, false, "", false, false, "")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, telegramUpdate{}, nil, nil)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; unrelated conversation must keep general scope", scope)
	}
	if writes := sideEffectNames(filtered); writes["pending_batch_decision"] || !writes["record_transaction"] {
		t.Fatalf("unrelated conversation got wrong mutation catalog: %v", writes)
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

func TestPendingActionDoesNotOwnOrdinaryTurn(t *testing.T) {
	var update telegramUpdate
	tools := AgentFinanceTools([]string{"dining"}, true, true, true, "AMBIGUOUS_CATEGORY", true, true, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}, &agentMerchantLearningBinding{ReviewRequestID: "merchant-review", TransactionID: "tx"})
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s", scope)
	}
	if !writes["record_transaction"] || writes["confirm_pending_action"] || writes["cancel_pending_action"] {
		t.Fatalf("ordinary turn got wrong catalog: %v", writes)
	}
}

func TestSalaryChoiceDoesNotOwnOrdinaryTurn(t *testing.T) {
	tools := AgentFinanceTools(nil, false, false, false, "", true, false, "")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, telegramUpdate{}, nil, nil)
	if scope != agentWorkflowGeneral || sideEffectNames(filtered)["resolve_salary_choice"] {
		t.Fatalf("ordinary turn scope=%s writes=%v", scope, sideEffectNames(filtered))
	}
}

func TestImplicitReviewBindingDoesNotNarrowOrdinaryTurn(t *testing.T) {
	var update telegramUpdate
	tools := AgentFinanceTools([]string{"dining"}, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}, nil)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; an ordinary turn must keep the general catalog", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("general catalog must be intact, got %v", side)
	}
}

func TestImplicitMerchantBindingDoesNotNarrowOrdinaryTurn(t *testing.T) {
	var update telegramUpdate
	tools := AgentFinanceTools([]string{"dining"}, false, false, false, "", false, true, "")
	filtered, scope := applyAgentWorkflowToolPolicy(tools, update, nil, &agentMerchantLearningBinding{ReviewRequestID: "r", TransactionID: "t"})
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; an ordinary turn must keep the general catalog", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("general catalog must be intact, got %v", side)
	}
}

func TestCycleResidualCardGivesTheAssistantNoMutationTool(t *testing.T) {
	tools := AgentFinanceTools(nil, false, false, true, "CYCLE_RESIDUAL_ALLOCATION", false, false, "CYCLE_RESIDUAL")
	for _, tool := range tools {
		if tool.Name == "resolve_review" {
			t.Fatalf("the assistant can still answer a cycle residual card with %s", tool.Name)
		}
	}
}
