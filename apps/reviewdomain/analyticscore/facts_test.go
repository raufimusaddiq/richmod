package analyticscore

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMedian3UsesMiddleCompletedCycle(t *testing.T) {
	if got := median3([]string{"1400000", "700000", "1350000"}); got == nil || *got != "1350000" {
		t.Fatalf("median=%v", got)
	}
	if median3([]string{"1", "2"}) != nil {
		t.Fatal("insufficient history must not expose a median")
	}
}

func TestChangeSeparatesPreviousCycleFromRecentMedian(t *testing.T) {
	previous, median := "700000", "1350000"
	change := change("1400000", &previous, &median)
	if change.Delta == nil || *change.Delta != "700000" || change.Relative == nil || *change.Relative != "1.0000" {
		t.Fatalf("previous-delta=%+v", change)
	}
	if change.DeltaMedian == nil || *change.DeltaMedian != "50000" || change.RelativeMedian == nil || *change.RelativeMedian != "0.0370" {
		t.Fatalf("median-delta=%+v", change)
	}
}

func TestRelativeChangeRetainsAbsoluteTinyDenominatorContext(t *testing.T) {
	if positiveRatio("1000", "0") != nil {
		t.Fatal("zero denominator must not produce a percentage")
	}
	previous := "1"
	c := change("1001", &previous, nil)
	if *c.Previous != "1" || c.Amount != "1001" || *c.Delta != "1000" || *c.Relative != "1000.0000" {
		t.Fatalf("absolute denominator context lost: %+v", c)
	}
}

func TestCashChangeUsesOnlyCompletedEligibleHistory(t *testing.T) {
	amount := func(c reviewCashflow) string { return c.Expense }
	measures := []cycleMeasure{
		{period: reviewPeriod{State: "CLOSED"}, cash: reviewCashflow{Expense: "1400000"}},
		{period: reviewPeriod{State: "CLOSED"}, cash: reviewCashflow{Expense: "700000"}},
		{period: reviewPeriod{State: "CLOSED"}, cash: reviewCashflow{Expense: "1500000"}},
		{period: reviewPeriod{State: "CLOSED"}, cash: reviewCashflow{Expense: "1350000"}},
	}
	result := cashChange(measures, amount)
	if result.Previous == nil || *result.Previous != "700000" || result.Median == nil || *result.Median != "1350000" {
		t.Fatalf("comparison=%+v", result)
	}
}

func TestReviewDoesNotAuthorSemanticConclusions(t *testing.T) {
	payload, err := json.Marshal(Facts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"noteworthy", "insight", "recommendation", "advice", "importance"} {
		if strings.Contains(strings.ToLower(string(payload)), forbidden) {
			t.Fatalf("deterministic facts must not carry semantic field %q", forbidden)
		}
	}
}

func TestCompletenessUsesGrossExpenseAndReviewCoverage(t *testing.T) {
	f := Facts{Cashflow: reviewCashflow{GrossExpense: "1000000", Refund: "500000"}, Quality: []reviewBlocker{{Kind: "UNCATEGORIZED_EXPENSE", Amount: valuePointer("250000")}}}
	if got := f.Completeness(); got != "0.7500" {
		t.Fatalf("coverage=%s", got)
	}
	f.Quality = append(f.Quality, reviewBlocker{Kind: "OPEN_REVIEWS", Count: 2})
	if got := f.Completeness(); got != "0.6750" {
		t.Fatalf("review-adjusted coverage=%s", got)
	}
	f.Cashflow.GrossExpense = "0"
	if f.Completeness() != "0.5000" {
		t.Fatal("empty cycle with reviews must not claim completeness")
	}
	f.Quality = nil
	if f.Completeness() != "1.0000" {
		t.Fatal("empty complete cycle coverage")
	}
}
