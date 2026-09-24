package review

import (
	"reflect"
	"testing"
)

// PRD §13: the Inbox is proposal-first. These unit tests fix the render contract
// on the API side, so the card always has a proposal, a single reason, and the
// list of genuinely unresolved facts regardless of which pipeline created it.
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

// A review stored before the decision contract existed has no proposal. It must
// keep working rather than rendering an empty card.
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

// The card decides what to ask for from the server-provided missing facts, so an
// interaction mode must never be able to request a field the decision did not
// name (PRD §13.4).
func TestProposalFactsIgnoresFactsNotNamedAsMissing(t *testing.T) {
	decision := []byte(`{"version":1,"reasonCode":"FINANCIAL_EMAIL_RESOLUTION","decisionClass":"EVIDENCE_GAP","knownFacts":{},"missingFacts":["wealth_account"],"provenance":{},"allowedActions":["SET_FINANCIAL_EMAIL_ENTITIES","IGNORE"],"interactionMode":"SINGLE_FIELD"}`)
	missing := proposalFacts(decision).MissingFacts
	for _, field := range missing {
		if field == "amount" || field == "merchant" || field == "transactionAt" {
			t.Fatalf("amount, merchant, and time are never re-requested: %v", missing)
		}
	}
}
