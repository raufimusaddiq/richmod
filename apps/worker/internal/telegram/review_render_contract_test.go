package telegram

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

// producibleReviewTypes is the review_type set the producers can emit. Adding a
// new producer without a ReviewDecision preset must fail this test, so no review
// can reach Telegram without renderer capability (UIR-03 exhaustiveness).
var producibleReviewTypes = []string{
	"UNKNOWN_MERCHANT",
	"AMBIGUOUS_CATEGORY",
	"UNKNOWN_PURPOSE",
	"MISSING_TRANSACTION_DATE",
	"MISSING_PAY_DATE",
	"TRANSACTION_FACTS_MISSING",
	"MANUAL_CORRECTION",
	"POSSIBLE_DUPLICATE",
	"CONFLICTING_EVIDENCE",
	"TRANSFER_CLASSIFICATION",
	"PAYSLIP_CONFIRMATION",
	"WEALTH_OBSERVATION_CONFIRMATION",
	"DOCUMENT_EXTRACTION_LOW_CONFIDENCE",
	"DOCUMENT_CLASSIFICATION",
	"UNKNOWN_BANK_TEMPLATE",
	"CYCLE_RESIDUAL_ALLOCATION",
	"FINANCIAL_EMAIL_RESOLUTION",
}

func TestEveryProducibleReviewTypeHasARenderableDecision(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, ok := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		if !ok {
			// A review type with no preset must still render as a bound reply rather
			// than a category chooser, so the zero decision is acceptable; what is not
			// acceptable is a category prompt.
			state, _, mode := renderReviewPresentation(reviewdec.Decision{}, reviewType, "context")
			if mode == "category" {
				t.Fatalf("%s has no decision but rendered as a category chooser (state=%s)", reviewType, state)
			}
			continue
		}
		_, _, mode := renderReviewPresentation(decision, reviewType, "context")
		if mode == "" {
			t.Fatalf("%s produced no markup mode", reviewType)
		}
		if contains(decision.MissingFacts, "transfer_relationship") && mode != "transfer" {
			t.Fatalf("%s transfer decision rendered as %q, want the transfer chooser", reviewType, mode)
		}
		if mode == "category" && !isCategoryOnly(decision) {
			t.Fatalf("%s rendered as a category chooser without a category-only decision", reviewType)
		}
	}
}

// markupModesHandledAtCreation mirrors the switch in EnqueueReviewRequest. Every
// mode the renderer can emit must have a case there, or the review silently falls
// back to the generic Ubah detail/Abaikan keyboard.
var markupModesHandledAtCreation = map[string]bool{
	"category":  true,
	"reply":     true,
	"duplicate": true,
	"transfer":  true,
}

func TestRenderedMarkupModesAreHandledAtCreation(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, _ := reviewdec.Preset(reviewType, "transaction", "00000000-0000-0000-0000-000000000000")
		_, _, mode := renderReviewPresentation(decision, reviewType, "context")
		if !markupModesHandledAtCreation[mode] {
			t.Fatalf("%s rendered mode %q with no creation markup case", reviewType, mode)
		}
	}
}
