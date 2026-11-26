package bankemail

import (
	"slices"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func TestUnknownMerchantDecisionAsksForMerchantFirst(t *testing.T) {
	amount, direction, channel := "54000", "OUTGOING", "DEBIT_CARD"
	d := transactionReviewDecision("h", "s", Extraction{AmountIDR: &amount, Direction: &direction, Channel: &channel, TransactionAt: &time.Time{}}, PolicyResult{ReviewType: "UNKNOWN_MERCHANT"}, false, "t")
	if len(d.MissingFacts) != 2 || d.MissingFacts[0] != "merchant" || d.MissingFacts[1] != "category" {
		t.Fatalf("merchant must precede category, got %v", d.MissingFacts)
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
	d := transactionReviewDecision("h", "s", Extraction{}, PolicyResult{ReviewType: "AMBIGUOUS_CATEGORY"}, false, "t")
	if d.InteractionMode != "SINGLE_FIELD" || len(d.AllowedActions) != 2 || d.AllowedActions[0] != "CONFIRM_REVIEW" || d.AllowedActions[1] != "IGNORE" {
		t.Fatalf("category dropdown contract=%+v", d)
	}
	if d.DecisionSource != reviewdec.SourceGenerativeExtraction {
		t.Fatalf("category decision source=%q", d.DecisionSource)
	}
}

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

func TestTransferClassificationDecisionOffersRelationshipChoices(t *testing.T) {
	d := transactionReviewDecision("h", "s", Extraction{}, PolicyResult{ReviewType: "TRANSFER_CLASSIFICATION"}, false, "t")
	preset, ok := reviewdec.Preset("TRANSFER_CLASSIFICATION", "transaction", "t")
	if !ok {
		t.Fatal("TRANSFER_CLASSIFICATION preset missing")
	}
	if !slices.Equal(d.AllowedActions, preset.AllowedActions) || !slices.Contains(d.AllowedActions, "CLASSIFY_TRANSFER") {
		t.Fatalf("allowed actions=%v, want %v", d.AllowedActions, preset.AllowedActions)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("decision must stay valid: %v", err)
	}
}
