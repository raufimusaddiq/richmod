package bankemail

import "testing"

// The missing-facts list must name every absent required fact, not just the
// first: an email with neither amount nor time must ask for both, so the Inbox
// does not silently leave one unresolved (PRD §7.3).
func TestPartialDecisionListsEveryMissingRequiredFact(t *testing.T) {
	decision := partialDecision("household", "source", Extraction{}, "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", []string{"amount", "transaction_at"}, "facts absent")
	if len(decision.MissingFacts) != 2 {
		t.Fatalf("missing facts=%v; want amount and transaction_at", decision.MissingFacts)
	}
	if decision.DecisionClass == "" || decision.WhyNotAuto == "" {
		t.Fatalf("decision must explain why it exists: %+v", decision)
	}
	if decision.Subject.ID != "source" {
		t.Fatalf("subject=%q", decision.Subject.ID)
	}
}
