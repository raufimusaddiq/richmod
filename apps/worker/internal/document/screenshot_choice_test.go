package document

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/reviewdec"
)

type stubRowVerifier struct {
	calls   int
	request judgment.Request
	answers map[string]judgment.Answer
}

func (s *stubRowVerifier) Evaluate(_ context.Context, _ string, request judgment.Request) (judgment.Result, error) {
	s.calls++
	s.request = request
	return judgment.Result{Model: "stub-jev", Answers: s.answers}, nil
}

var rowCategoryOptions = []categoryOption{{ID: "cat-food", Slug: "food-and-drink"}, {ID: "cat-groceries", Slug: "groceries"}}

// rowAnswerFor builds a complete distribution over the server-owned criteria:
// one confident top choice and the remaining mass split over the others.
func rowAnswerFor(choice string, top float64) judgment.Answer {
	distribution := map[string]float64{}
	for _, key := range []string{"food-and-drink", "groceries", "OTHER_OR_UNCLEAR"} {
		if key == choice {
			distribution[key] = top
		} else {
			distribution[key] = (1 - top) / 2
		}
	}
	return judgment.Answer{Type: "choice", Choice: choice, Distribution: distribution, Confidence: 0.9, HasConfidence: true}
}

func unmatchedOutRow(amount string) validatedScreenshotRow {
	return validatedScreenshotRow{Value: screenshotRow{Direction: "OUT", Amount: amount, Currency: "IDR", Merchant: "Warung", Confidence: .95}, Type: "EXPENSE", DateKnown: true}
}

func TestScreenshotDuplicateUsesSharedReviewContract(t *testing.T) {
	// PRD §37: every screenshot reason code must store the one contract that
	// reason resolves to, or the Inbox hides an action the API still accepts.
	for _, reason := range []string{"POSSIBLE_DUPLICATE", "AMBIGUOUS_CATEGORY"} {
		got := screenshotRowDecision("household", "event", "transaction", reason, 0, validatedScreenshotRow{})
		want, ok := reviewdec.Preset(reason, "transaction", "transaction")
		if !ok {
			t.Fatalf("missing shared preset for %s", reason)
		}
		if got.ReasonCode != reason || got.DecisionClass != want.DecisionClass || got.InteractionMode != want.InteractionMode || !reflect.DeepEqual(got.MissingFacts, want.MissingFacts) || !reflect.DeepEqual(got.AllowedActions, want.AllowedActions) {
			t.Fatalf("screenshot %s must use the shared contract: got=%+v want=%+v", reason, got, want)
		}
	}
}

// PRD §11.3: every unmatched row of one image shares a single bounded request.
func TestResolveRowCategoriesBatchesOneRequestPerImage(t *testing.T) {
	verifier := &stubRowVerifier{answers: map[string]judgment.Answer{"row_000": rowAnswerFor("food-and-drink", 0.9), "row_002": {Type: "choice", Choice: "", Confidence: 0, HasConfidence: false}}}
	rows := []validatedScreenshotRow{unmatchedOutRow("54000"), unmatchedOutRow("25000"), {Value: screenshotRow{Direction: "IN", Amount: "100000", Currency: "IDR", Confidence: .95}, Type: "INCOME", DateKnown: true}}
	decided, provenance, err := (&Processor{verifier: verifier}).resolveRowCategories(context.Background(), "evt", rows, rowCategoryOptions)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.calls != 1 {
		t.Fatalf("rows of one image must share one bounded request, got %d calls", verifier.calls)
	}
	if len(verifier.request.Questions) != 2 {
		t.Fatalf("only unmatched OUT rows are questioned, got %d", len(verifier.request.Questions))
	}
	if decided[0] != "cat-food" {
		t.Fatalf("a decisive answer must map back to the canonical category, got %q", decided[0])
	}
	if _, ok := decided[2]; ok {
		t.Fatal("an incoming row has no bounded category decision")
	}
	if provenance.Decided != 1 || provenance.Questions != 2 || provenance.Model != "stub-jev" {
		t.Fatalf("unexpected provenance %+v", provenance)
	}
}

// A coin-flip answer is the EVIDENCE_GAP case the PRD wants one question for.
func TestResolveRowCategoriesLeavesThinMarginUndecided(t *testing.T) {
	verifier := &stubRowVerifier{answers: map[string]judgment.Answer{"row_000": rowAnswerFor("food-and-drink", 0.55)}}
	decided, _, err := (&Processor{verifier: verifier}).resolveRowCategories(context.Background(), "evt", []validatedScreenshotRow{unmatchedOutRow("25000")}, rowCategoryOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(decided) != 0 {
		t.Fatalf("a thin-margin answer must not decide a category: %v", decided)
	}
}

// Without the bounded plane nothing may auto-confirm (PRD §17, fail closed).
func TestResolveRowCategoriesWithoutVerifierDecidesNothing(t *testing.T) {
	decided, provenance, err := (&Processor{}).resolveRowCategories(context.Background(), "evt", []validatedScreenshotRow{unmatchedOutRow("25000")}, rowCategoryOptions)
	if err != nil || len(decided) != 0 || provenance.Questions != 0 {
		t.Fatalf("no verifier must decide nothing: decided=%v provenance=%+v err=%v", decided, provenance, err)
	}
}

// PRD §11.1/§17: an unmatched OUT row reaches canonical state only with a
// decisive category, a printed date, and no unresolved conflict; an incoming row
// never does (PRD §11.5).
func TestScreenshotRowAutoConfirmConditions(t *testing.T) {
	category := "cat-food"
	decided := unmatchedOutRow("54000")
	decided.CategoryID, decided.CategoryDecided = &category, true
	cases := []struct {
		name string
		row  validatedScreenshotRow
		want bool
	}{
		{"decisive category with printed date", decided, true},
		{"undecided category", unmatchedOutRow("54000"), false},
		{"incoming row", func() validatedScreenshotRow {
			row := decided
			row.Type = "INCOME"
			row.CategoryID = nil
			return row
		}(), false},
		{"missing printed date", func() validatedScreenshotRow {
			row := decided
			row.DateKnown = false
			return row
		}(), false},
		{"conflicting category sources", func() validatedScreenshotRow {
			row := decided
			row.CategoryConflict = true
			return row
		}(), false},
		{"low extraction confidence", func() validatedScreenshotRow {
			row := decided
			row.Value.Confidence = .85
			return row
		}(), false},
	}
	for _, testCase := range cases {
		if got := testCase.row.autoConfirmable(); got != testCase.want {
			t.Fatalf("%s: autoConfirmable=%v want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestScreenshotSummaryReportsBatchOutcome(t *testing.T) {
	summary := screenshotSummary(10, 8, 1, 1)
	for _, want := range []string{"10 transaksi ditemukan.", "✓ 8 berhasil dicatat", "✓ 1 cocok dengan transaksi yang sudah ada", "! 1 butuh keputusan"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q must contain %q", summary, want)
		}
	}
	if clean := screenshotSummary(2, 2, 0, 0); strings.Contains(clean, "butuh keputusan") {
		t.Fatalf("a fully recorded batch asks for nothing: %q", clean)
	}
}
