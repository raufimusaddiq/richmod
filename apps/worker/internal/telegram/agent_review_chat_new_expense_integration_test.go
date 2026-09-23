package telegram

import (
	"context"
	"testing"
)

// Stage 3 regression guard (ADR-038 amendment). The production defect was a
// chat that held an open review swallowing a later, clearly-new expense: the
// tool catalog was narrowed to resolve_review from stale chat state and the
// turn died with zero model phases. Route-first selection must keep the general
// catalog for the new expense so it can be recorded exactly once.
func TestOpenReviewChatRecordsNewExpenseWithGeneralCatalog(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "review-chat-new-expense")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))

	general := agentFinanceTools([]string{"dining"}, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION", true)
	review := &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}

	// The route for "jajan gorengan 5k" is CREATE_TRANSACTION, so the open
	// review must not narrow the catalog and must not be attached to the turn.
	filtered, scope := applyAgentWorkflowToolPolicy(general, f.update, review, nil, "CREATE_TRANSACTION")
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; an open review must not own a new-transaction turn", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("record_transaction must stay available, got %v", side)
	}

	// The handler agrees: with no binding attached, it does not consume the turn.
	p := NewProcessor(f.pool, nil)
	p.SetJudgment(nonReviewJudgmentEngine{})
	state := &agentState{HouseholdID: f.householdID, SourceEventID: f.sourceID, Update: f.update, Tools: filtered, TurnContext: map[string]any{}}
	handled, err := p.tryJudgmentBoundWorkflow(ctx, state, "jajan gorengan 5k")
	mustAgentTest(t, err)
	if handled {
		t.Fatal("a new expense must fall through to the general path, not be swallowed by stale chat state")
	}
}
