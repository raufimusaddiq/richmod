package telegram

import (
	"testing"
)

// The ambiguity claim is inverted: only an extraction that is affirmatively
// complete and unambiguous opens the auto-confirm path, and an exact
// deterministic merchant-category match is exempt because it never asked.
func TestUndecidedAmbiguityDoesNotAuthorizeConfirmation(t *testing.T) {
	decided := TransactionSemanticDecision{
		RouteAccepted: true, TransactionType: "EXPENSE", TypeAccepted: true,
		AmountSupported: true, DateSupported: true, CategorySlug: "dining", CategoryAccepted: true,
		AmbiguityDecidedNotAmbiguous: true, DecisionSource: "GENERATIVE_EXTRACTION", PolicyVersion: decisionPolicyVersion,
	}
	if !decided.decisionAllowed() {
		t.Fatalf("a decided not-ambiguous ruling must authorize: %+v", decided)
	}

	undecided := decided
	undecided.AmbiguityDecidedNotAmbiguous = false
	if undecided.decisionAllowed() {
		t.Fatalf("an undecided ambiguity ruling must fail closed: %+v", undecided)
	}

	deterministic := undecided
	deterministic.DecisionSource = "DETERMINISTIC_POLICY"
	if !deterministic.decisionAllowed() {
		t.Fatalf("an exact deterministic match is unambiguous by construction: %+v", deterministic)
	}
}

// The tool catalog must classify every exposed tool, so a new mutation tool
// cannot silently bypass the read/side-effect policy.
func TestEveryExposedMutationToolIsClassified(t *testing.T) {
	for _, tool := range AgentFinanceTools([]string{"dining"}, true, true, true, "TRANSFER_CLASSIFICATION", true, true, "TRANSFER_CLASSIFICATION") {
		if _, known := agentToolClassFor(tool.Name); !known {
			t.Fatalf("tool %q has no agent tool class", tool.Name)
		}
	}
}
