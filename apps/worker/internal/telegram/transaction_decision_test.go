package telegram

import (
	"testing"
)

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

func TestEveryExposedMutationToolIsClassified(t *testing.T) {
	for _, tool := range AgentFinanceTools([]string{"dining"}, true, true) {
		if _, known := agentToolClassFor(tool.Name); !known {
			t.Fatalf("tool %q has no agent tool class", tool.Name)
		}
	}
}
