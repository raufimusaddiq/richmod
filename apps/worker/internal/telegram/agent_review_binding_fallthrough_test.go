package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// undecidedJudgmentEngine returns no usable decision, simulating a classifier
// that cannot decide (or is unavailable).
type undecidedJudgmentEngine struct{ err error }

func (e undecidedJudgmentEngine) Evaluate(_ context.Context, _ string, _ judgment.Request) (judgment.Result, error) {
	if e.err != nil {
		return judgment.Result{}, e.err
	}
	return judgment.Result{Model: "stub-jev", Answers: map[string]judgment.Answer{}}, nil
}

// nonReviewJudgmentEngine reports that the user text is not a review answer
// (OTHER_OR_UNCLEAR) while answering everything else affirmatively.
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

// A chat-level review binding must not swallow a clearly-new transaction. When
// the review classifier says the message is not a review answer, the turn must
// fall through to normal handling with the general tool catalog restored.
func TestImplicitReviewBindingFallsThroughForNewTransaction(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "review-fallthrough")

	general := agentFinanceTools(nil, false, false, true, "UNKNOWN_MERCHANT", false, false, "AWAITING_MERCHANT", true)
	filtered, scope := applyAgentWorkflowToolPolicy(general, f.update, &agentReviewBinding{Kind: "TRANSACTION", TargetID: "t", ReviewRequestID: "r"}, nil)
	if scope != agentWorkflowUniqueReview || len(filtered) >= len(general) {
		t.Fatalf("expected the implicit review binding to narrow the catalog, scope=%s", scope)
	}

	p := NewProcessor(f.pool, nil)
	p.SetJudgment(nonReviewJudgmentEngine{})
	state := &agentState{
		HouseholdID:      f.householdID,
		SourceEventID:    f.sourceID,
		Update:           f.update,
		WorkflowScope:    string(scope),
		GeneralTools:     general,
		Tools:            filtered,
		ReviewBinding:    &agentReviewBinding{Kind: "TRANSACTION", TargetID: "t", ReviewRequestID: "r"},
		ReviewBindingCount: 1,
		ReviewMode:       "AWAITING_MERCHANT",
		TurnContext:      map[string]any{"active_review": map[string]any{"review_mode": "AWAITING_MERCHANT"}},
	}
	handled, err := p.tryJudgmentBoundWorkflow(ctx, state, "jajan gorengan 5k")
	mustAgentTest(t, err)
	if handled {
		t.Fatal("a non-review message must fall through, not be swallowed by the review binding")
	}
	if state.ReviewBinding != nil {
		t.Fatal("the implicit review binding must be dropped on fallthrough")
	}
	if side := sideEffectNames(state.Tools); !side["record_transaction"] {
		t.Fatalf("general side-effect catalog must be restored, got %v", side)
	}
}

// The explicit-reply case keeps the hard stop: there the user does mean to
// answer the review, so an unreadable reply must not silently become a new
// transaction.
func TestExplicitReplyReviewBindingStaysBound(t *testing.T) {
	ctx := context.Background()
	f := newAgentIntegrationFixture(t, "review-explicit")
	p := NewProcessor(f.pool, nil)
	p.SetJudgment(nonReviewJudgmentEngine{})
	state := &agentState{
		HouseholdID:      f.householdID,
		SourceEventID:    f.sourceID,
		Update:           f.update,
		WorkflowScope:    string(agentWorkflowExactReview),
		GeneralTools:     agentFinanceTools(nil, false, false, true, "UNKNOWN_MERCHANT", false, false, "AWAITING_MERCHANT", true),
		ReviewBinding:    &agentReviewBinding{Kind: "TRANSACTION", TargetID: "t", ReviewRequestID: "r"},
		ReviewBindingCount: 1,
		ReviewMode:       "AWAITING_MERCHANT",
		TurnContext:      map[string]any{"active_review": map[string]any{"review_mode": "AWAITING_MERCHANT"}},
	}
	handled, err := p.tryJudgmentBoundWorkflow(ctx, state, "gajelas")
	mustAgentTest(t, err)
	if !handled {
		t.Fatal("an unreadable explicit review reply must be consumed with a clarification")
	}
	if state.ReviewBinding == nil {
		t.Fatal("an explicit review reply must not drop its binding")
	}
}

// An undecided or unavailable classifier must keep the clarification hard stop:
// there is no evidence the message is a new event, so mutation tools must not be
// reopened on the strength of an ambiguous result.
func TestImplicitReviewBindingStaysBoundWhenClassifierUndecided(t *testing.T) {
	for name, engine := range map[string]judgment.Engine{
		"undecided": undecidedJudgmentEngine{},
		"error":     undecidedJudgmentEngine{err: errors.New("engine unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgentIntegrationFixture(t, "review-undecided-"+name)
			p := NewProcessor(f.pool, nil)
			p.SetJudgment(engine)
			state := &agentState{
				HouseholdID:        f.householdID,
				SourceEventID:      f.sourceID,
				Update:             f.update,
				WorkflowScope:      string(agentWorkflowUniqueReview),
				GeneralTools:       agentFinanceTools(nil, false, false, true, "UNKNOWN_MERCHANT", false, false, "AWAITING_MERCHANT", true),
				ReviewBinding:      &agentReviewBinding{Kind: "TRANSACTION", TargetID: "t", ReviewRequestID: "r"},
				ReviewBindingCount: 1,
				ReviewMode:         "AWAITING_MERCHANT",
				TurnContext:        map[string]any{"active_review": map[string]any{"review_mode": "AWAITING_MERCHANT"}},
			}
			handled, err := p.tryJudgmentBoundWorkflow(ctx, state, "jajan gorengan 5k")
			mustAgentTest(t, err)
			if !handled {
				t.Fatal("an undecided classifier must keep the clarification hard stop")
			}
			if state.ReviewBinding == nil {
				t.Fatal("the binding must not be dropped on an undecided result")
			}
		})
	}
}
