package review

import (
	"testing"
)

// PRD §13: the Inbox is proposal-first. These unit tests fix the render contract
// on the API side, so the card always has a proposal, a single reason, and the
// list of genuinely unresolved facts regardless of which pipeline created it.
func TestProposalFactsFromDecisionCarriesClassAndMissingFacts(t *testing.T) {
	decision := []byte(`{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","decisionClass":"EVIDENCE_GAP","knownFacts":{"amount_idr":"54000"},"proposedFacts":{"categorySlug":"food-and-drink"},"missingFacts":["category"],"whyNotAutoConfirm":"thin margin","allowedActions":["IGNORE"],"interactionMode":"BOUNDED_CHOICE"}`)
	proposal, missing := proposalFacts(decision)
	if proposal["categorySlug"] != "food-and-drink" {
		t.Fatalf("the proposed fact must drive the card: %+v", proposal)
	}
	if len(missing) != 1 || missing[0] != "category" {
		t.Fatalf("only the unresolved dimension may be requested: %+v", missing)
	}
}

// A review stored before the decision contract existed has no proposal. It must
// keep working rather than rendering an empty card.
func TestProposalFactsWithoutDecisionReturnsNoMissingFacts(t *testing.T) {
	proposal, missing := proposalFacts(nil)
	if len(proposal) != 0 || len(missing) != 0 {
		t.Fatalf("a decision-less review must not invent a proposal: %+v %+v", proposal, missing)
	}
	proposal, missing = proposalFacts([]byte("not json"))
	if len(proposal) != 0 || len(missing) != 0 {
		t.Fatalf("an unreadable decision must not invent a proposal: %+v %+v", proposal, missing)
	}
}

// The card decides what to ask for from the server-provided missing facts, so an
// interaction mode must never be able to request a field the decision did not
// name (PRD §13.4).
func TestProposalFactsIgnoresFactsNotNamedAsMissing(t *testing.T) {
	decision := []byte(`{"version":1,"reasonCode":"FINANCIAL_EMAIL_RESOLUTION","decisionClass":"EVIDENCE_GAP","knownFacts":{},"missingFacts":["wealth_account"],"provenance":{},"allowedActions":["SET_FINANCIAL_EMAIL_ENTITIES","IGNORE"],"interactionMode":"SINGLE_FIELD"}`)
	_, missing := proposalFacts(decision)
	for _, field := range missing {
		if field == "amount" || field == "merchant" || field == "transactionAt" {
			t.Fatalf("amount, merchant, and time are never re-requested: %v", missing)
		}
	}
}

