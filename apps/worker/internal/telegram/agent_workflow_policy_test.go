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

func repliesTo(messageID int64) telegramUpdate {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: messageID}
	return update
}

func TestExplicitReviewReplyGivesTheAssistantNoWriteTool(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, true, true)
	filtered, scope := applyAgentWorkflowToolPolicy(tools, repliesTo(77), &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"})
	if scope != agentWorkflowExactReview {
		t.Fatalf("scope=%s", scope)
	}
	if writes := sideEffectNames(filtered); len(writes) != 0 {
		t.Fatalf("a bound card reply exposed writes: %v", writes)
	}
}

func TestExplicitWealthReplyOffersOnlyTheAssetPurchaseProposal(t *testing.T) {
	tools := append(AgentFinanceTools([]string{"dining"}, true, true), proposeAssetPurchaseTool())
	filtered, scope := applyAgentWorkflowToolPolicy(tools, repliesTo(78), &agentReviewBinding{Kind: wealthObservationBindingKind, TargetID: "target", ReviewRequestID: "review"})
	if scope != agentWorkflowExactReview {
		t.Fatalf("scope=%s", scope)
	}
	if writes := sideEffectNames(filtered); len(writes) != 1 || !writes[toolProposeAssetPurchase] {
		t.Fatalf("writes=%v; want only %s", writes, toolProposeAssetPurchase)
	}
}

func TestPendingBatchDoesNotOwnUnrelatedAgentTurn(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, false, true)
	filtered, scope := applyAgentWorkflowToolPolicy(tools, telegramUpdate{}, nil)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; unrelated conversation must keep general scope", scope)
	}
	if writes := sideEffectNames(filtered); writes["pending_batch_decision"] || !writes["record_transaction"] {
		t.Fatalf("unrelated conversation got wrong mutation catalog: %v", writes)
	}
}

func TestStaleExplicitReplyExposesNoSideEffects(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, true, true)
	filtered, scope := applyAgentWorkflowToolPolicy(tools, repliesTo(99), nil)
	if scope != agentWorkflowExplicitUnbound {
		t.Fatalf("scope=%s", scope)
	}
	if writes := sideEffectNames(filtered); len(writes) != 0 {
		t.Fatalf("stale explicit reply exposed side effects: %v", writes)
	}
}

func TestPendingActionDoesNotOwnOrdinaryTurn(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, true, true)
	filtered, scope := applyAgentWorkflowToolPolicy(tools, telegramUpdate{}, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"})
	writes := sideEffectNames(filtered)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s", scope)
	}
	if !writes["record_transaction"] || writes["confirm_pending_action"] || writes["cancel_pending_action"] {
		t.Fatalf("ordinary turn got wrong catalog: %v", writes)
	}
}

func TestImplicitReviewBindingDoesNotNarrowOrdinaryTurn(t *testing.T) {
	tools := AgentFinanceTools([]string{"dining"}, false, false)
	filtered, scope := applyAgentWorkflowToolPolicy(tools, telegramUpdate{}, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"})
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; an ordinary turn must keep the general catalog", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("general catalog must be intact, got %v", side)
	}
}

func TestNoCardAnswerToolIsOfferedInAnyWorkflowState(t *testing.T) {
	removed := []string{"resolve_review", "resolve_salary_choice", "resolve_merchant_learning"}
	bindings := []*agentReviewBinding{
		nil,
		{Kind: "TRANSACTION", TargetID: "t", ReviewRequestID: "r"},
		{Kind: "CYCLE_RESIDUAL", TargetID: "t", ReviewRequestID: "r"},
		{Kind: "TRANSFER_RECONCILIATION", TargetID: "t", ReviewRequestID: "r"},
		{Kind: "BANK_FACTS", TargetID: "t", ReviewRequestID: "r"},
		{Kind: wealthObservationBindingKind, TargetID: "t", ReviewRequestID: "r"},
	}
	for _, pendingAction := range []bool{false, true} {
		for _, pendingBatch := range []bool{false, true} {
			base := append(AgentFinanceTools([]string{"dining"}, pendingAction, pendingBatch), showReviewCardTool(), proposeAssetPurchaseTool())
			for _, binding := range bindings {
				for _, update := range []telegramUpdate{{}, repliesTo(5)} {
					tools, _ := applyAgentWorkflowToolPolicy(base, update, binding)
					for _, name := range removed {
						if agentToolAvailable(tools, name) || agentToolAvailable(base, name) {
							t.Fatalf("%s offered (action=%v batch=%v binding=%v)", name, pendingAction, pendingBatch, binding)
						}
					}
					if _, known := agentToolClassFor("resolve_review"); known {
						t.Fatal("resolve_review is still a registered tool")
					}
				}
			}
		}
	}
}
