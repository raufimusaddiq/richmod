package telegram

import "testing"

func TestEveryServerOwnedRouteMapsToExactlyOneLane(t *testing.T) {
	want := map[string]agentRouteLane{
		"READ_SPENDING":                 laneFastPathTerminal,
		"READ_CASHFLOW":                 laneFastPathTerminal,
		"READ_SAVINGS":                  laneFastPathTerminal,
		"READ_WEALTH":                   laneFastPathTerminal,
		"SEARCH_TRANSACTIONS":           laneAgentFallthrough,
		"CREATE_TRANSACTION":            laneFastPathTerminal,
		"CREATE_TRANSFER":               laneAgentFallthrough,
		"CORRECT_TRANSACTION":           laneAgentFallthrough,
		"REVIEW_INTERACTION":            laneWorkflow,
		"SALARY_INTERACTION":            laneWorkflow,
		"MERCHANT_LEARNING_INTERACTION": laneWorkflow,
		"FINANCE_HELP":                  laneAgentFallthrough,
		"NEEDS_GENERATIVE_AGENT":        laneAgentFallthrough,
		"OUT_OF_SCOPE":                  laneOutOfScope,
		"OTHER_OR_UNCLEAR":              laneClarification,
	}

	if len(judgmentRoutes) != len(want) {
		t.Fatalf("route vocabulary has %d entries; lane contract has %d", len(judgmentRoutes), len(want))
	}
	seen := make(map[string]bool, len(judgmentRoutes))
	for _, route := range judgmentRoutes {
		if seen[route] {
			t.Fatalf("duplicate server-owned route %q", route)
		}
		seen[route] = true
		lane, ok := laneForRoute(route)
		if !ok {
			t.Errorf("route %q has no lane", route)
			continue
		}
		if lane != want[route] {
			t.Errorf("route %q lane=%q; want %q", route, lane, want[route])
		}
	}
	for route := range want {
		if !seen[route] {
			t.Errorf("lane contract contains route %q absent from server vocabulary", route)
		}
	}
	if _, ok := laneForRoute("FUTURE_UNDECLARED_ROUTE"); ok {
		t.Fatal("undeclared route must not acquire a lane")
	}
}

func TestAgentRoutesFallThroughAndOnlyUnclearOrOutOfScopeTerminate(t *testing.T) {
	for route, lane := range agentRouteLanes {
		switch lane {
		case laneAgentFallthrough, laneWorkflow:
			if route == "OTHER_OR_UNCLEAR" || route == "OUT_OF_SCOPE" {
				t.Errorf("terminal route %q incorrectly falls through", route)
			}
		case laneClarification, laneOutOfScope:
			if route != "OTHER_OR_UNCLEAR" && route != "OUT_OF_SCOPE" {
				t.Errorf("valid route %q terminates instead of falling through", route)
			}
		case laneFastPathTerminal:
			// Explicitly owned by tryJudgmentFastPath.
		default:
			t.Errorf("route %q has invalid lane %q", route, lane)
		}
	}
	for _, route := range []string{"CREATE_TRANSFER", "SEARCH_TRANSACTIONS", "CORRECT_TRANSACTION", "FINANCE_HELP", "NEEDS_GENERATIVE_AGENT"} {
		if lane, _ := laneForRoute(route); lane != laneAgentFallthrough {
			t.Errorf("%s lane=%q, want %q", route, lane, laneAgentFallthrough)
		}
	}
}
