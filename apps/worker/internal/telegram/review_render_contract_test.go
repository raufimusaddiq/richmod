package telegram

import (
	"strings"
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
	"SALARY_SOURCE_CONFIRMATION",
	"RECEIPT_MISMATCH",
	"INVOICE_PAYMENT_STATUS",
	"UNKNOWN_EMAIL_TEMPLATE",
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
	"category":        true,
	"reply":           true,
	"duplicate":       true,
	"salary":          true,
	"transfer":        true,
	"document":        true,
	"financial_email": true,
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

func TestPayslipReviewUsesIndonesianPrompts(t *testing.T) {
	for _, reviewType := range []string{"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE"} {
		decision, ok := reviewdec.Preset(reviewType, "source_event", "00000000-0000-0000-0000-000000000000")
		if !ok {
			t.Fatalf("missing preset for %s", reviewType)
		}
		_, message, _ := renderReviewPresentation(decision, reviewType, "")
		if strings.Contains(message, "salary") || strings.Contains(message, "transaction date") {
			t.Fatalf("%s has non-Indonesian UI: %q", reviewType, message)
		}
		if reviewType == "MISSING_PAY_DATE" && !strings.Contains(message, "25 September 2026") {
			t.Fatalf("missing Indonesian date example: %q", message)
		}
	}
}

func TestTransactionDatePromptKeepsItsSupportedFormat(t *testing.T) {
	decision, _ := reviewdec.Preset("MISSING_TRANSACTION_DATE", "transaction", "00000000-0000-0000-0000-000000000000")
	_, message, _ := renderReviewPresentation(decision, "MISSING_TRANSACTION_DATE", "")
	if !strings.Contains(message, "tanggal transaksi (YYYY-MM-DD)") || strings.Contains(message, "September") {
		t.Fatalf("transaction date prompt does not match its parser: %q", message)
	}
}

func TestPayslipReviewTypesCanProject(t *testing.T) {
	for _, kind := range []string{"PAYSLIP_CONFIRMATION", "MISSING_PAY_DATE"} {
		if !TelegramCompletableReviewType(kind) {
			t.Fatalf("%s cannot project", kind)
		}
	}
}

// TestSuppliedContextKeepsItsMarkupMode proves the UIR-02 shared projection does
// not drop a review's supplied prompt: when a producer supplies its own message,
// the decision still selects the markup and state, so a source/document review
// arrives as an actionable card instead of an unanswerable notice. A category or
// transfer review uses the provider's summary as the card body unchanged; a
// detail/date/duplicate review wraps the summary in the decision prompt.
func TestSuppliedContextKeepsItsMarkupMode(t *testing.T) {
	for _, reviewType := range producibleReviewTypes {
		decision, ok := reviewdec.Preset(reviewType, "source_event", "00000000-0000-0000-0000-000000000000")
		if !ok {
			continue
		}
		state, message, mode := renderReviewPresentation(decision, reviewType, "bespoke prompt")
		if state == "" || mode == "" {
			t.Fatalf("%s lost its state/markup with a supplied prompt", reviewType)
		}
		if !strings.Contains(message, "bespoke prompt") {
			t.Fatalf("%s dropped the supplied prompt: %q", reviewType, message)
		}
		// The mode must follow the decision, not the review type: a supplied
		// prompt must not turn a bounded chooser into a free-form reply or back.
		wantMode := "reply"
		switch {
		case contains(decision.AllowedActions, "REPROCESS_DOCUMENT"):
			wantMode = "document"
		case contains(decision.AllowedActions, "SET_FINANCIAL_EMAIL_ENTITIES"):
			wantMode = "financial_email"
		case isCategoryOnly(decision):
			wantMode = "category"
		case contains(decision.MissingFacts, "transfer_relationship"):
			wantMode = "transfer"
		case decision.InteractionMode == reviewdec.ModeConflictResolution || contains(decision.MissingFacts, "duplicate_relationship"):
			wantMode = "duplicate"
		case contains(decision.MissingFacts, "salary_classification") && contains(decision.AllowedActions, "PRIMARY_SALARY") && contains(decision.AllowedActions, "ORDINARY_INCOME"):
			wantMode = "salary"
		}
		if mode != wantMode {
			t.Fatalf("%s rendered mode %q with a supplied prompt, want %q", reviewType, mode, wantMode)
		}
	}
}
