package telegram

import (
	"context"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// Route-first lane selection (ADR-038 amendment): an implicit workflow binding
// is attached only when the route names that interaction. A new expense in a
// chat that happens to hold an open review must keep the general catalog and
// must not be narrowed to resolve_review. This replaces the per-binding
// fall-through special case removed in this change.
func TestRouteFirstKeepsGeneralCatalogForNewTransaction(t *testing.T) {
	var update telegramUpdate
	general := agentFinanceTools(nil, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION", true)
	review := &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}

	// Route says this is a new transaction, so the review binding is invisible.
	filtered, scope := applyAgentWorkflowToolPolicy(general, update, review, nil, "CREATE_TRANSACTION")
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; a CREATE_TRANSACTION route must keep the general catalog", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("record_transaction must stay available, got %v", side)
	}

	// Route names the review interaction, so the binding narrows as before.
	filtered, scope = applyAgentWorkflowToolPolicy(general, update, review, nil, "REVIEW_INTERACTION")
	if scope != agentWorkflowUniqueReview {
		t.Fatalf("scope=%s; a REVIEW_INTERACTION route must narrow to the review", scope)
	}
	if side := sideEffectNames(filtered); len(side) != 1 || !side["resolve_review"] {
		t.Fatalf("writes=%v; want resolve_review only", side)
	}
}

// The merchant-learning binding follows the same route-first rule.
func TestRouteFirstMerchantBindingFollowsRoute(t *testing.T) {
	var update telegramUpdate
	general := agentFinanceTools(nil, false, false, false, "", false, true, "", true)
	merchant := &agentMerchantLearningBinding{ReviewRequestID: "r", TransactionID: "t"}

	filtered, scope := applyAgentWorkflowToolPolicy(general, update, nil, merchant, "CREATE_TRANSACTION")
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; a CREATE_TRANSACTION route must keep the general catalog", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("general catalog must be intact, got %v", side)
	}

	filtered, scope = applyAgentWorkflowToolPolicy(general, update, nil, merchant, "MERCHANT_LEARNING_INTERACTION")
	if scope != agentWorkflowMerchantLearning {
		t.Fatalf("scope=%s; a MERCHANT_LEARNING_INTERACTION route must narrow to it", scope)
	}
	if side := sideEffectNames(filtered); len(side) != 1 || !side["resolve_merchant_learning"] {
		t.Fatalf("writes=%v; want resolve_merchant_learning only", side)
	}
}

// An exact binding (an explicit reply) outranks the route: the user is
// unambiguously answering the workflow message, so the lane must stay bound
// even though the route cannot see the reply target.
func TestExactReplyBindingOutranksRoute(t *testing.T) {
	var update telegramUpdate
	update.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: 77}
	general := agentFinanceTools(nil, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION", true)
	review := &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}
	_, scope := applyAgentWorkflowToolPolicy(general, update, review, nil, "CREATE_TRANSACTION")
	if scope != agentWorkflowExactReview {
		t.Fatalf("scope=%s; an explicit reply must keep its exact binding", scope)
	}
}

// A classifier that returns no usable route leaves Route empty, so no implicit
// binding may narrow the catalog. The turn fails closed on the general path
// rather than silently owning itself with stale chat state.
func TestEmptyRouteDoesNotNarrowImplicitBinding(t *testing.T) {
	var update telegramUpdate
	general := agentFinanceTools(nil, false, false, true, "AMBIGUOUS_CATEGORY", false, false, "TRANSACTION", true)
	review := &agentReviewBinding{Kind: "TRANSACTION", TargetID: "target", ReviewRequestID: "review"}
	filtered, scope := applyAgentWorkflowToolPolicy(general, update, review, nil, "")
	if scope != agentWorkflowGeneral {
		t.Fatalf("scope=%s; an undecided route must not narrow an implicit binding", scope)
	}
	if side := sideEffectNames(filtered); !side["record_transaction"] {
		t.Fatalf("general catalog must be intact, got %v", side)
	}
}

// undecidedJudgmentEngine returns no usable decision, simulating a classifier
// that cannot decide (or is unavailable).
type undecidedJudgmentEngine struct{ err error }

func (e undecidedJudgmentEngine) Evaluate(_ context.Context, _ string, _ judgment.Request) (judgment.Result, error) {
	if e.err != nil {
		return judgment.Result{}, e.err
	}
	return judgment.Result{Model: "stub-jev", Answers: map[string]judgment.Answer{}}, nil
}

// nonReviewJudgmentEngine answers OTHER_OR_UNCLEAR whenever the question offers
// it, and affirmatively otherwise. It simulates a classifier that is confident
// the message is not a bounded answer to the pending workflow.
type nonReviewJudgmentEngine struct{}

func (nonReviewJudgmentEngine) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	answers := map[string]judgment.Answer{}
	for key, question := range request.Questions {
		criteria, ok := question.Criteria.(map[string]any)
		if !ok {
			answers[key] = decidedNoul(0.99)
			continue
		}
		if _, has := criteria["OTHER_OR_UNCLEAR"]; has {
			answers[key] = confidentChoice(criteria, "OTHER_OR_UNCLEAR")
			continue
		}
		labels := judgment.CriteriaLabels(criteria)
		if len(labels) > 0 {
			answers[key] = confidentChoice(criteria, labels[0])
		}
	}
	return judgment.Result{Model: "stub-jev", Answers: answers}, nil
}
