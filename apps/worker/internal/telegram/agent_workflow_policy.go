package telegram

import "github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"

type agentWorkflowScope string

const (
	agentWorkflowGeneral          agentWorkflowScope = "GENERAL"
	agentWorkflowExactReview      agentWorkflowScope = "EXACT_REVIEW"
	agentWorkflowExactMerchant    agentWorkflowScope = "EXACT_MERCHANT_LEARNING"
	agentWorkflowExplicitUnbound  agentWorkflowScope = "EXPLICIT_REPLY_UNBOUND"
)

// applyAgentWorkflowToolPolicy enforces server-owned target precedence before
// the model sees the tool catalog. An explicit Telegram reply is authoritative:
// it may resolve only the exactly bound workflow. A stale/unrelated explicit
// reply fails closed and exposes no side-effect tool at all.
func applyAgentWorkflowToolPolicy(
	tools []gateway.ToolDefinition,
	update telegramUpdate,
	reviewBinding *agentReviewBinding,
	merchantBinding *agentMerchantLearningBinding,
) ([]gateway.ToolDefinition, agentWorkflowScope) {
	if update.Message.ReplyToMessage == nil || update.Message.ReplyToMessage.MessageID == 0 {
		return tools, agentWorkflowGeneral
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
