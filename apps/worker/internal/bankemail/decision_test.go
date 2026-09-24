package bankemail

import (
	"testing"
	"time"
)

// A missing merchant must not become a required text field (PRD §9.4/§9.5):
// what the review actually asks for is a category, and the known facts stay
// read-only so the user is never asked to re-enter amount, time, or direction.
func TestUnknownMerchantDecisionAsksForCategoryOnly(t *testing.T) {
	amount, direction, channel := "54000", "OUTGOING", "DEBIT_CARD"
	d := transactionReviewDecision("h", "s", Extraction{AmountIDR: &amount, Direction: &direction, Channel: &channel, TransactionAt: &time.Time{}}, PolicyResult{ReviewType: "UNKNOWN_MERCHANT"}, "t")
	if len(d.MissingFacts) != 1 || d.MissingFacts[0] != "category" {
		t.Fatalf("an undecided category is the missing fact, got %v", d.MissingFacts)
	}
	if d.PolicyVersion != ToolSchemaVersion {
		t.Fatalf("deterministic merchant policy version=%q", d.PolicyVersion)
	}
	if d.InteractionMode != "SINGLE_FIELD" || len(d.AllowedActions) != 2 {
		t.Fatalf("category gap needs a field plus honest actions: %+v", d)
	}
	for _, fact := range []string{"amount_idr", "transaction_at", "direction", "channel"} {
		if d.KnownFacts[fact] == nil {
			t.Fatalf("known fact %s must travel with the review: %+v", fact, d.KnownFacts)
		}
		for _, missing := range d.MissingFacts {
			if missing == fact {
				t.Fatalf("known fact %s must never be requested again", fact)
			}
		}
	}
}

func TestAmbiguousCategoryDecisionMatchesInbox(t *testing.T) {
	d := transactionReviewDecision("h", "s", Extraction{}, PolicyResult{ReviewType: "AMBIGUOUS_CATEGORY"}, "t")
	if d.InteractionMode != "SINGLE_FIELD" || len(d.AllowedActions) != 2 || d.AllowedActions[0] != "CONFIRM_REVIEW" || d.AllowedActions[1] != "IGNORE" {
		t.Fatalf("category dropdown contract=%+v", d)
	}
	if d.DecisionSource != "GENERATIVE_PLUS_JEV" {
		t.Fatalf("category decision source=%q", d.DecisionSource)
	}
}

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
