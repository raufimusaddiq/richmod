package telegram

import (
	"context"
	"testing"
)

// Regression: an open review swallowed a later new expense because the catalog was narrowed from stale chat
// state and the turn died with zero model phases.
func TestOpenReviewChatRecordsNewExpenseWithGeneralCatalog(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "review-chat-new-expense")
	var categoryID string
	mustAgentTest(t, f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, f.householdID).Scan(&categoryID))

	general := agentFinanceTools([]string{"dining"}, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION")
	review := &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}

	filtered, scope := applyAgentWorkflowToolPolicy(general, f.update, review, nil)
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; an open review must not own a new-transaction turn", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("record_transaction must stay available, got %v", side)
	}
}
