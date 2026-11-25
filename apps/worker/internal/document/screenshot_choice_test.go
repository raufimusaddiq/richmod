package document

import (
	"reflect"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/merchantmemory"
)

func unmatchedOutRow(amount string) validatedScreenshotRow {
	return validatedScreenshotRow{Value: screenshotRow{Direction: "OUT", Amount: &amount, Currency: "IDR", Merchant: "Warung", Confidence: .95, CategoryConfidence: .95}, Type: "EXPENSE", DateKnown: true}
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
		{"merchant memory overrides a disagreeing low-confidence model category", func() validatedScreenshotRow {
			row := decided
			modelCategory := "cat-meals"
			row.CategoryID, row.CategorySlug, row.Value.CategoryConfidence = &modelCategory, "makanan-minuman", .7
			row.applyMerchantMemory(merchantmemory.Match{MerchantID: "m-calorie", CategoryID: "cat-snacks"}, "kopi-camilan")
			if *row.CategoryID != "cat-snacks" || row.CategorySlug != "kopi-camilan" {
				t.Fatalf("memory did not win: %+v", row)
			}
			return row
		}(), true},
		{"extraction confidence at the floor", func() validatedScreenshotRow {
			row := decided
			row.Value.Confidence = .85
			return row
		}(), true},
		{"extraction confidence below the floor", func() validatedScreenshotRow {
			row := decided
			row.Value.Confidence = .84
			return row
		}(), false},
		{"model category confidence below the floor", func() validatedScreenshotRow {
			row := decided
			row.Value.CategoryConfidence = .5
			return row
		}(), false},
		{"merchant memory category ignores model category confidence", func() validatedScreenshotRow {
			row := decided
			row.Value.CategoryConfidence = .1
			row.CategoryFromMemory = true
			return row
		}(), true},
		{"merchant memory category with low extraction confidence", func() validatedScreenshotRow {
			row := decided
			row.Value.Confidence = .5
			row.CategoryFromMemory = true
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

func TestScreenshotClassificationConfidenceFollowsCategorySource(t *testing.T) {
	row := unmatchedOutRow("1000")
	row.Value.Confidence, row.Value.CategoryConfidence = .9, .7
	if got := row.classificationConfidence(); got != .7 {
		t.Fatalf("model category confidence must be persisted, got %v", got)
	}
	row.CategoryFromMemory = true
	if got := row.classificationConfidence(); got != 1 {
		t.Fatalf("merchant memory category is decisive, got %v", got)
	}
}
