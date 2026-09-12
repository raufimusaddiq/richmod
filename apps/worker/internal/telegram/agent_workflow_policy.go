package telegram

import "github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"

type agentWorkflowScope string

const (
	agentWorkflowGeneral          agentWorkflowScope = "GENERAL"
	agentWorkflowExactReview      agentWorkflowScope = "EXACT_REVIEW"
	agentWorkflowExactMerchant    agentWorkflowScope = "EXACT_MERCHANT_LEARNING"
	agentWorkflowExplicitUnbound  agentWorkflowScope = "EXPLICIT_REPLY_UNBOUND"
	agentWorkflowPendingAction    agentWorkflowScope = "PENDING_ACTION"
	agentWorkflowPendingBatch     agentWorkflowScope = "PENDING_BATCH"
	agentWorkflowUniqueReview     agentWorkflowScope = "UNIQUE_REVIEW"
	agentWorkflowMerchantLearning agentWorkflowScope = "MERCHANT_LEARNING"
	agentWorkflowSalaryChoice     agentWorkflowScope = "SALARY_CHOICE"
)

// applyAgentWorkflowToolPolicy enforces server-owned target precedence before
// the model sees the tool catalog. READ tools always remain available. When Go
// already knows a durable workflow target, only that workflow's SIDE_EFFECT
// tools are exposed; the model may understand the reply but cannot select a
// different mutation lane.
func applyAgentWorkflowToolPolicy(
	tools []gateway.ToolDefinition,
	update telegramUpdate,
	reviewBinding *agentReviewBinding,
	merchantBinding *agentMerchantLearningBinding,
) ([]gateway.ToolDefinition, agentWorkflowScope) {
	available := func(name string) bool {
		for _, tool := range tools {
			if tool.Name == name {
				return true
			}
		}
		return false
	}

	allowed := map[string]bool{}
	scope := agentWorkflowGeneral
	explicitReply := update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.MessageID != 0
	if explicitReply {
		scope = agentWorkflowExplicitUnbound
		switch {
		case reviewBinding != nil:
			allowed["resolve_review"] = true
			scope = agentWorkflowExactReview
		case merchantBinding != nil:
			allowed["resolve_merchant_learning"] = true
			scope = agentWorkflowExactMerchant
		}
	} else {
		// Pending correction/batch states are exact chat+user scoped workflows and
		// outrank implicit review selection. After those, unique server-bound
		// reviews and other durable confirmation workflows outrank general writes.
		switch {
		case available("confirm_pending_action") || available("cancel_pending_action"):
			allowed["confirm_pending_action"] = true
			allowed["cancel_pending_action"] = true
			scope = agentWorkflowPendingAction
		case available("confirm_pending_batch") || available("cancel_pending_batch") || available("update_pending_batch"):
			allowed["confirm_pending_batch"] = true
			allowed["cancel_pending_batch"] = true
			allowed["update_pending_batch"] = true
			scope = agentWorkflowPendingBatch
		case reviewBinding != nil:
			allowed["resolve_review"] = true
			scope = agentWorkflowUniqueReview
		case merchantBinding != nil:
			allowed["resolve_merchant_learning"] = true
			scope = agentWorkflowMerchantLearning
		case available("resolve_salary_choice"):
			allowed["resolve_salary_choice"] = true
			scope = agentWorkflowSalaryChoice
		default:
			return tools, agentWorkflowGeneral
		}
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
