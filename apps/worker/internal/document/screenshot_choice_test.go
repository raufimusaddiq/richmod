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
		got := screenshotRowDecision("household", "event", "transaction", reason, 0, validatedScreenshotRow{Type: "EXPENSE", DateKnown: true}, false)
		want, ok := reviewdec.Preset(reason, "transaction", "transaction")
		if !ok {
			t.Fatalf("missing shared preset for %s", reason)
		}
		if got.ReasonCode != reason || got.DecisionClass != want.DecisionClass || got.InteractionMode != want.InteractionMode || !reflect.DeepEqual(got.MissingFacts, want.MissingFacts) || !reflect.DeepEqual(got.AllowedActions, want.AllowedActions) {
			t.Fatalf("screenshot %s must use the shared contract: got=%+v want=%+v", reason, got, want)
		}
	}
}

func TestScreenshotReviewDecisionNamesOnlyResidualFacts(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		row          validatedScreenshotRow
		want         []string
	}{
		{"date only", "MISSING_TRANSACTION_DATE", validatedScreenshotRow{Type: "EXPENSE", CategoryID: ptr("food"), CategoryDecided: true, Value: screenshotRow{CategorySlug: ptr("food")}}, []string{"transaction_at"}},
		{"category and date", "TRANSACTION_FACTS_MISSING", validatedScreenshotRow{Type: "EXPENSE"}, []string{"category", "transaction_at"}},
		{"category conflict and date", "AMBIGUOUS_CATEGORY", validatedScreenshotRow{Type: "EXPENSE", DateKnown: true, CategoryConflict: true}, []string{"category"}},
		{"category conflict and missing date", "TRANSACTION_FACTS_MISSING", validatedScreenshotRow{Type: "EXPENSE", CategoryConflict: true}, []string{"category", "transaction_at"}},
		{"duplicate takes priority", "POSSIBLE_DUPLICATE", validatedScreenshotRow{Type: "EXPENSE"}, []string{"duplicate_relationship"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := screenshotRowDecision("h", "s", "t", test.reason, 0, test.row, false)
			if !reflect.DeepEqual(got.MissingFacts, test.want) {
				t.Fatalf("missing facts=%v; want %v", got.MissingFacts, test.want)
			}
		})
	}
}

// PRD §11.3: every unmatched row of one image shares a single bounded request.
func TestResolveRowCategoriesBatchesOneRequestPerImage(t *testing.T) {
	verifier := &stubRowVerifier{answers: map[string]judgment.Answer{"row_017": rowAnswerFor("food-and-drink", 0.9), "row_018": rowAnswerFor("groceries", 0.9), "row_019": {Type: "choice", Choice: "", Confidence: 0, HasConfidence: false}}}
	rows := make([]validatedScreenshotRow, 20)
	for i := range rows {
		rows[i] = unmatchedOutRow("25000")
		if i < 17 {
			rows[i].CategoryID, rows[i].CategoryDecided = ptr("cat-food"), true
		}
	}
	decided, provenance, err := (&Processor{verifier: verifier}).resolveRowCategories(context.Background(), "evt", rows, rowCategoryOptions)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.calls != 1 {
		t.Fatalf("rows of one image must share one bounded request, got %d calls", verifier.calls)
	}
	if len(verifier.request.Questions) != 3 {
		t.Fatalf("only the 3 unresolved OUT rows are questioned, got %d", len(verifier.request.Questions))
	}
	if len(decided) != 2 || decided[17] != "cat-food" || decided[18] != "cat-groceries" {
		t.Fatalf("two decisive residuals must map to canonical categories: %v", decided)
	}
	for i := 0; i < 17; i++ {
		if _, ok := decided[i]; ok {
			t.Fatalf("clear row %d was re-judged", i)
		}
	}
	if provenance.Decided != 2 || provenance.Questions != 3 || provenance.Model != "stub-jev" {
		t.Fatalf("unexpected selective batch provenance %+v", provenance)
	}
}

func TestClearScreenshotRowsNeverEnterBatch(t *testing.T) {
	verifier := &stubRowVerifier{}
	clear := unmatchedOutRow("1000")
	clear.CategoryID, clear.CategoryDecided = ptr("cat-food"), true
	decided, provenance, err := (&Processor{verifier: verifier}).resolveRowCategories(context.Background(), "evt", []validatedScreenshotRow{clear}, rowCategoryOptions)
	if err != nil || verifier.calls != 0 || len(decided) != 0 || provenance.Questions != 0 {
		t.Fatalf("clear row must skip Jev: calls=%d decisions=%v provenance=%+v err=%v", verifier.calls, decided, provenance, err)
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
