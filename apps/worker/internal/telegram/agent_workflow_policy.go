package telegram

import "github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"

type agentWorkflowScope string

const (
	agentWorkflowGeneral         agentWorkflowScope = "GENERAL"
	agentWorkflowExactReview     agentWorkflowScope = "EXACT_REVIEW"
	agentWorkflowExactMerchant   agentWorkflowScope = "EXACT_MERCHANT_LEARNING"
	agentWorkflowExplicitUnbound agentWorkflowScope = "EXPLICIT_REPLY_UNBOUND"
)

func applyAgentWorkflowToolPolicy(
	tools []gateway.ToolDefinition,
	update telegramUpdate,
	reviewBinding *agentReviewBinding,
	merchantBinding *agentMerchantLearningBinding,
) ([]gateway.ToolDefinition, agentWorkflowScope) {
	explicitReply := update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0
	if !explicitReply {
		return withoutWorkflowTools(tools), agentWorkflowGeneral
	}
	allowed := map[string]bool{}
	scope := agentWorkflowExplicitUnbound
	switch {
	case reviewBinding != nil:
		allowed["resolve_review"] = true
		scope = agentWorkflowExactReview
	case merchantBinding != nil:
		allowed["resolve_merchant_learning"] = true
		scope = agentWorkflowExactMerchant
	}
	filtered := make([]gateway.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if pendingBatchTool(tool.Name) {
			continue
		}
		class, known := agentToolClassFor(tool.Name)
		if !known || class == agentToolRead {
			filtered = append(filtered, tool)
			continue
		}
		if allowed[tool.Name] {
			filtered = append(filtered, tool)
		}
	}
	return filtered, scope
}

func pendingBatchTool(name string) bool { return name == "pending_batch_decision" }

func workflowBoundTool(name string) bool {
	switch name {
	case "confirm_pending_action", "cancel_pending_action", "pending_batch_decision", "resolve_review",
		"resolve_salary_choice", "resolve_merchant_learning":
		return true
	default:
		return false
	}
}

func withoutWorkflowTools(tools []gateway.ToolDefinition) []gateway.ToolDefinition {
	out := make([]gateway.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if workflowBoundTool(tool.Name) {
			continue
		}
		out = append(out, tool)
	}
	return out
}
