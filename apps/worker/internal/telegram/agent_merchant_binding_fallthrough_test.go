package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// A chat-level merchant-learning binding must not swallow a clearly-new
// transaction, mirroring the implicit review-binding fall-through fixed in PR
// #115. When the classifier says the message is not an answer to the merchant
// confirmation, the turn falls through with the general tool catalog restored.
func TestImplicitMerchantBindingFallsThroughForNewTransaction(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "merchant-fallthrough")

	general := agentFinanceTools(nil, false, false, true, "UNKNOWN_MERCHANT", false, false, "AWAITING_MERCHANT", true)
	filtered, scope := applyAgentWorkflowToolPolicy(general, f.update, nil, &agentMerchantLearningBinding{ReviewRequestID: "r", TransactionID: "t"})
	if scope != agentWorkflowMerchantLearning || len(filtered) >= len(general) {
		t.Fatalf("expected the implicit merchant binding to narrow the catalog, scope=%s", scope)
	}

	p := NewProcessor(f.pool, nil)
	p.SetJudgment(nonReviewJudgmentEngine{})
	state := &agentState{
		HouseholdID:             f.householdID,
		SourceEventID:           f.sourceID,
		Update:                  f.update,
		WorkflowScope:           string(scope),
		GeneralTools:            general,
		Tools:                   filtered,
		MerchantLearningBinding: &agentMerchantLearningBinding{ReviewRequestID: "r", TransactionID: "t"},
		MerchantLearningCount:   1,
		TurnContext:             map[string]any{"merchant_learning": map[string]any{"merchant": "Jajan gorengan"}},
	}
	handled, err := p.tryJudgmentBoundWorkflow(ctx, state, "jajan gorengan 5k")
	mustAgentTest(t, err)
	if handled {
		t.Fatal("a non-answer message must fall through, not be swallowed by the merchant binding")
	}
	if state.MerchantLearningBinding != nil {
		t.Fatal("the implicit merchant binding must be dropped on fallthrough")
	}
	if side := sideEffectNames(state.Tools); !side["record_transaction"] {
		t.Fatalf("general side-effect catalog must be restored, got %v", side)
	}
}

// An explicit reply to the merchant confirmation keeps the hard stop: there the
// user does mean to answer, so an undecided or unreadable classifier result must
// not silently become a new transaction.
func TestExplicitMerchantReplyStaysBound(t *testing.T) {
	for name, engine := range map[string]judgment.Engine{
		"undecided": undecidedJudgmentEngine{},
		"error":     undecidedJudgmentEngine{err: errors.New("engine unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "merchant-explicit-"+name)
			p := NewProcessor(f.pool, nil)
			p.SetJudgment(engine)
			state := &agentState{
				HouseholdID:             f.householdID,
				SourceEventID:           f.sourceID,
				Update:                  f.update,
				WorkflowScope:           string(agentWorkflowExactMerchant),
				GeneralTools:            agentFinanceTools(nil, false, false, true, "UNKNOWN_MERCHANT", false, false, "AWAITING_MERCHANT", true),
				MerchantLearningBinding: &agentMerchantLearningBinding{ReviewRequestID: "r", TransactionID: "t"},
				MerchantLearningCount:   1,
				TurnContext:             map[string]any{"merchant_learning": map[string]any{"merchant": "Jajan gorengan"}},
			}
			handled, err := p.tryJudgmentBoundWorkflow(ctx, state, "gajelas")
			mustAgentTest(t, err)
			if !handled {
				t.Fatal("an unreadable explicit merchant reply must be consumed with a clarification")
			}
			if state.MerchantLearningBinding == nil {
				t.Fatal("an explicit merchant reply must not drop its binding")
			}
		})
	}
}
