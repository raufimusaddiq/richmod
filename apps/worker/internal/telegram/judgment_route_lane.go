package telegram

// agentRouteLane is the single server-owned classification of every route in
// judgmentRoutes (PRD §8.1/§8.2). Each route maps to exactly one lane. Adding a
// new route to judgmentRoutes without a lane here fails
// TestEveryServerOwnedRouteMapsToExactlyOneLane, which is the guard the PRD
// requires: a future route cannot silently fall into a generic "unclear" reply.
type agentRouteLane string

const (
	// laneFastPathTerminal: the bounded fast path answers and ends the turn.
	laneFastPathTerminal agentRouteLane = "FAST_PATH_TERMINAL"
	// laneWorkflow: the agent receives a route-gated server-owned workflow.
	laneWorkflow agentRouteLane = "WORKFLOW"
	// laneAgentFallthrough: the fast path declines, so the conversational agent
	// handles the turn with its full tool surface. Terminating these as unclear
	// is the defect PRD §8.1 prohibits.
	laneAgentFallthrough agentRouteLane = "AGENT_FALLTHROUGH"
	// laneClarification: no safe route; ask a bounded clarification.
	laneClarification agentRouteLane = "CLARIFICATION"
	// laneOutOfScope: terminal scoped response for non-finance requests.
	laneOutOfScope agentRouteLane = "OUT_OF_SCOPE"
)

// agentRouteLanes is the exhaustive table. Every entry of judgmentRoutes must
// appear here; the regression test enforces that both directions stay in sync.
var agentRouteLanes = map[string]agentRouteLane{
	// The fast path terminally serves aggregate reads and the simple
	// harvested-transaction and review-list replies.
	"READ_SPENDING":      laneFastPathTerminal,
	"READ_CASHFLOW":      laneFastPathTerminal,
	"READ_SAVINGS":       laneFastPathTerminal,
	"READ_WEALTH":        laneFastPathTerminal,
	"CREATE_TRANSACTION": laneFastPathTerminal,

	// These need arbitrary extraction, reasoning, tools, or prose. The fast path
	// must return control to the conversational agent rather than terminating.
	"SEARCH_TRANSACTIONS":    laneAgentFallthrough,
	"CREATE_TRANSFER":        laneAgentFallthrough,
	"CORRECT_TRANSACTION":    laneAgentFallthrough,
	"FINANCE_HELP":           laneAgentFallthrough,
	"NEEDS_GENERATIVE_AGENT": laneAgentFallthrough,

	// Route-gated workflows are bound by applyAgentWorkflowToolPolicy after this
	// route has been decided.
	"REVIEW_INTERACTION":            laneWorkflow,
	"SALARY_INTERACTION":            laneWorkflow,
	"MERCHANT_LEARNING_INTERACTION": laneWorkflow,

	// Terminal scope refusal and bounded clarification.
	"OUT_OF_SCOPE":     laneOutOfScope,
	"OTHER_OR_UNCLEAR": laneClarification,
}

// laneForRoute reports the lane for a decided route. The second return is false
// for any route the server vocabulary does not define, so callers fail closed.
func laneForRoute(route string) (agentRouteLane, bool) {
	lane, ok := agentRouteLanes[route]
	return lane, ok
}
