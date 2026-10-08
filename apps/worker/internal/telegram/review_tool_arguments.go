package telegram

import (
	"slices"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

// reviewActionArguments names the resolve_review arguments each bound
// transaction or bank-fact action reads in its executor
// (agentResolveBoundTransactionReview, agentResolveBoundBankFacts). CONFIRM is
// left out: what it reads depends on what the card asks for.
var reviewActionArguments = map[string][]string{
	"EXPENSE":             {"category_slug"},
	"ASSET_PURCHASE":      {"wealth_account_hint"},
	"MERGE_EXISTING":      {"candidate_ref"},
	"SET_PAY_DATE":        {"pay_date", "transaction_at"},
	"COMPLETE_BANK_FACTS": {"amount_idr", "transaction_at"},
}

// confirmArguments is what CONFIRM reads for a card waiting in a given state: the
// typed value the card asked for, or the whole confirm surface when the card asks
// for nothing specific.
func confirmArguments(conversationState string) []string {
	switch conversationState {
	case "AWAITING_DATE":
		return []string{"transaction_at"}
	case "AWAITING_MERCHANT":
		return []string{"merchant", "category_slug"}
	case "AWAITING_DETAIL":
		return []string{"description"}
	case "AWAITING_CATEGORY":
		return []string{"category_slug"}
	}
	return []string{"category_slug", "description", "merchant", "pay_date", "transaction_at"}
}

// focusReviewArguments narrows resolve_review to the arguments the bound card's
// actions read, so the model cannot put the household's answer in a field the
// executor ignores. Bindings with their own executors (wealth, residual,
// transfer reconciliation) keep the full set.
func focusReviewArguments(tools []gateway.ToolDefinition, binding *agentReviewBinding) {
	if binding == nil || (binding.Kind != "TRANSACTION" && binding.Kind != "BANK_FACTS") {
		return
	}
	for index := range tools {
		if tools[index].Name != "resolve_review" {
			continue
		}
		properties, ok := tools[index].Parameters["properties"].(map[string]any)
		if !ok {
			return
		}
		actionSchema, _ := properties["action"].(map[string]any)
		actions, _ := actionSchema["enum"].([]string)
		keep := []string{"action"}
		for _, action := range actions {
			fields := reviewActionArguments[action]
			if action == "CONFIRM" {
				fields = confirmArguments(binding.ConversationState)
			}
			for _, field := range fields {
				if _, offered := properties[field]; offered && !slices.Contains(keep, field) {
					keep = append(keep, field)
				}
			}
		}
		for name := range properties {
			if !slices.Contains(keep, name) {
				delete(properties, name)
			}
		}
		slices.Sort(keep)
		tools[index].Parameters["required"] = keep
	}
}
