package review

import (
	"reflect"
	"testing"
)

func TestProposalFactsFromDecisionCarriesClassAndMissingFacts(t *testing.T) {
	decision := []byte(`{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","knownFacts":{"amount_idr":"54000"},"proposedFacts":{"categorySlug":"food-and-drink"},"missingFacts":["category"],"whyNotAutoConfirm":"thin margin","allowedActions":["IGNORE"],"interactionMode":"BOUNDED_CHOICE"}`)
	stored := proposalFacts(decision)
	if stored.ProposedFacts["categorySlug"] != "food-and-drink" {
		t.Fatalf("the proposed fact must drive the card: %+v", stored.ProposedFacts)
	}
	if stored.KnownFacts["amount_idr"] != "54000" {
		t.Fatalf("a known fact must reach the card so it is not re-asked: %+v", stored.KnownFacts)
	}
	if len(stored.MissingFacts) != 1 || stored.MissingFacts[0] != "category" {
		t.Fatalf("only the unresolved dimension may be requested: %+v", stored.MissingFacts)
	}
}

func TestConfirmationBlockersPreserveOnlyUnresolvedResiduals(t *testing.T) {
	decision := []byte(`{"missingFacts":["category","transaction_at"]}`)
	if got := confirmationBlockers(decision, false, false, false); !reflect.DeepEqual(got, []string{"category", "transaction_at"}) {
		t.Fatalf("blockers=%v", got)
	}
	if got := confirmationBlockers(decision, true, false, false); !reflect.DeepEqual(got, []string{"category"}) {
		t.Fatalf("blockers=%v; date should be resolved", got)
	}
	if got := confirmationBlockers(decision, true, true, false); len(got) != 0 {
		t.Fatalf("all residuals supplied, blockers=%v", got)
	}
}

func TestProposalFactsWithoutDecisionReturnsNoMissingFacts(t *testing.T) {
	stored := proposalFacts(nil)
	if len(stored.ProposedFacts) != 0 || len(stored.MissingFacts) != 0 || len(stored.KnownFacts) != 0 {
		t.Fatalf("a decision-less review must not invent a proposal: %+v", stored)
	}
	stored = proposalFacts([]byte("not json"))
	if len(stored.ProposedFacts) != 0 || len(stored.MissingFacts) != 0 || len(stored.KnownFacts) != 0 {
		t.Fatalf("an unreadable decision must not invent a proposal: %+v", stored)
	}
}

func TestProposalFactsIgnoresFactsNotNamedAsMissing(t *testing.T) {
	decision := []byte(`{"version":1,"reasonCode":"FINANCIAL_EMAIL_RESOLUTION","decisionClass":"EVIDENCE_GAP","knownFacts":{},"missingFacts":["wealth_account"],"provenance":{},"allowedActions":["SET_FINANCIAL_EMAIL_ENTITIES","IGNORE"],"interactionMode":"SINGLE_FIELD"}`)
	missing := proposalFacts(decision).MissingFacts
	for _, field := range missing {
		if field == "amount" || field == "merchant" || field == "transactionAt" {
			t.Fatalf("amount, merchant, and time are never re-requested: %v", missing)
		}
	}
}
