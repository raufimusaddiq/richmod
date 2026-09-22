package financialemail

import (
	"context"
	"errors"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// TestReconcileSemanticallyOnlyAcceptsAnAffirmativeHighNoul is the PRD §20
// invariant: Go narrows candidates deterministically, and the bounded plane may
// reuse a surviving ledger row only on an affirmative yes. An undecided middle
// band must keep the case in Review, because reusing the wrong row silently
// merges two real financial events.
func TestReconcileSemanticallyOnlyAcceptsAnAffirmativeHighNoul(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		answers    map[string]judgment.Answer
		wantSame   bool
		wantErr    bool
		wantCalled bool
	}{
		{"affirmative high noul reuses the row", map[string]judgment.Answer{"same_real_event": noul(0.95)}, true, false, true},
		{"low noul leaves the review", map[string]judgment.Answer{"same_real_event": noul(0.05)}, false, false, true},
		{"undecided middle leaves the review", map[string]judgment.Answer{"same_real_event": noul(0.50)}, false, false, true},
		{"missing answer leaves the review", map[string]judgment.Answer{}, false, false, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			verifier := &stubVerifier{answers: testCase.answers}
			processor := &Processor{verifier: verifier}
			answer, err := processor.reconcileSemantically(context.Background(), "req-1", "3000000", "2026-09-22T10:00:00+07:00", "Financial provider email", "UNCLASSIFIED", "", "2026-09-22T10:03:00+07:00")
			if testCase.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !testCase.wantErr && err != nil {
				t.Fatal(err)
			}
			if answer.SameEvent != testCase.wantSame {
				t.Fatalf("SameEvent = %v, want %v", answer.SameEvent, testCase.wantSame)
			}
			if verifier.calls != 1 {
				t.Fatalf("verifier calls = %d, want 1", verifier.calls)
			}
			if _, ok := verifier.request.Questions["same_real_event"]; !ok {
				t.Fatalf("the same-event question must be asked: %v", verifier.request.Questions)
			}
		})
	}
}

// TestReconcileSemanticallyWithoutAPlaneStaysInReview pins the fail-closed
// default: with no bounded plane configured, nothing is silently merged and the
// existing Review path still runs (ADR-038).
func TestReconcileSemanticallyWithoutAPlaneStaysInReview(t *testing.T) {
	answer, err := (&Processor{}).reconcileSemantically(context.Background(), "req-1", "3000000", "2026-09-22T10:00:00+07:00", "desc", "UNCLASSIFIED", "", "2026-09-22T10:03:00+07:00")
	if err != nil {
		t.Fatal(err)
	}
	if answer.SameEvent {
		t.Fatal("an unconfigured plane must never authorise reuse")
	}
}

// TestReconcileSemanticallyPropagatesProviderFailure keeps a provider outage
// from being read as a negative ruling, so the caller can distinguish "no" from
// "could not ask".
func TestReconcileSemanticallyPropagatesProviderFailure(t *testing.T) {
	processor := &Processor{verifier: &stubVerifier{err: errors.New("gateway down")}}
	if _, err := processor.reconcileSemantically(context.Background(), "req-1", "1", "at", "d", "UNCLASSIFIED", "", "at"); err == nil {
		t.Fatal("a provider failure must surface, not be read as a no")
	}
}

// TestCanReconcileSemanticallyRequiresExactlyOneCandidate is the §20 narrowing
// rule: many survivors stay ambiguous and belong to Review, not to a forced
// bounded choice, and an already-reused row is not re-ruled on.
func TestCanReconcileSemanticallyRequiresExactlyOneCandidate(t *testing.T) {
	for _, testCase := range []struct {
		name string
		plan cashPlan
		want bool
	}{
		{"one survivor is eligible", cashPlan{review: "TRANSFER_RECONCILIATION", candidates: []string{"a"}}, true},
		{"many survivors stay ambiguous", cashPlan{review: "TRANSFER_RECONCILIATION", candidates: []string{"a", "b"}}, false},
		{"already reused is not re-ruled", cashPlan{review: "TRANSFER_RECONCILIATION", candidates: []string{"a"}, existing: "a"}, false},
		{"a different review reason is untouched", cashPlan{review: "TRANSFER_CLASSIFICATION", candidates: []string{"a"}}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.plan.canReconcileSemantically(); got != testCase.want {
				t.Fatalf("canReconcileSemantically() = %v, want %v", got, testCase.want)
			}
		})
	}
}
