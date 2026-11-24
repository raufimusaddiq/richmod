package document

import (
	"reflect"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
)

func unmatchedOutRow(amount string) validatedScreenshotRow {
	return validatedScreenshotRow{Value: screenshotRow{Direction: "OUT", Amount: &amount, Currency: "IDR", Merchant: "Warung", Confidence: .95}, Type: "EXPENSE", DateKnown: true}
}

func TestScreenshotDuplicateUsesSharedReviewContract(t *testing.T) {
	for _, reason := range []string{"POSSIBLE_DUPLICATE", "AMBIGUOUS_CATEGORY"} {
		got := screenshotRowDecision("household", "event", "transaction", reason, 0, validatedScreenshotRow{Type: "EXPENSE", DateKnown: true})
		want, ok := reviewdec.Preset(reason, "transaction", "transaction")
		if !ok {
			t.Fatalf("missing shared preset for %s", reason)
		}
		if reason == "POSSIBLE_DUPLICATE" {
			want.MissingFacts = append(want.MissingFacts, "category")
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
		{"duplicate retains facts", "POSSIBLE_DUPLICATE", validatedScreenshotRow{Type: "EXPENSE"}, []string{"duplicate_relationship", "category", "transaction_at"}},
		{"incoming retains date", "TRANSFER_CLASSIFICATION", validatedScreenshotRow{Type: "INCOME"}, []string{"transfer_relationship", "transaction_at"}},
		{"missing amount only", "MISSING_AMOUNT", validatedScreenshotRow{Type: "EXPENSE", DateKnown: true, CategoryID: ptr("food"), CategoryDecided: true, Value: screenshotRow{CategorySlug: ptr("food")}}, []string{"amount"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := screenshotRowDecision("h", "s", "t", test.reason, 0, test.row)
			if !reflect.DeepEqual(got.MissingFacts, test.want) {
				t.Fatalf("missing facts=%v; want %v", got.MissingFacts, test.want)
			}
		})
	}
}

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
		}(), true},
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
