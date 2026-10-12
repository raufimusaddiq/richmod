package telegram

import (
	"context"
	"testing"
)

// Stage 3 regression guard (ADR-038 amendment). The production defect was a
// chat that held an open review swallowing a later, clearly-new expense: the
// tool catalog was narrowed to resolve_review from stale chat state and the
// turn died with zero model phases. An ordinary turn must keep the general
// catalog so the new expense can be recorded exactly once.
func TestOpenReviewChatRecordsNewExpenseWithGeneralCatalog(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "review-chat-new-expense")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))

	general := agentFinanceTools([]string{"dining"}, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION")
	review := &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}

	// An ordinary (non-reply) turn keeps the general catalog even while a
	// review is open, so the new expense can be recorded.
	filtered, scope := applyAgentWorkflowToolPolicy(general, f.update, review, nil)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; an open review must not own a new-transaction turn", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("record_transaction must stay available, got %v", side)
	}
}
