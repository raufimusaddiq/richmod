package analyticscore

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"
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
	full := cycleMeasure{cash: reviewCashflow{Expense: "9000000"}}
	result := cashChange(measures, amount, &full)
	if result.Previous == nil || *result.Previous != "700000" || result.Median == nil || *result.Median != "1350000" || *result.PreviousFull != "9000000" || *result.DeltaFull != "-7600000" {
		t.Fatalf("comparison=%+v", result)
	}
}

func TestFullOnlyCategoryDoesNotInventEligibleHistory(t *testing.T) {
	current := newCycleMeasure(reviewPeriod{})
	full := newCycleMeasure(reviewPeriod{})
	full.categories["rent"] = reviewValue{ID: "rent", Name: "Rent", Amount: "1950000"}
	rows := changes([]cycleMeasure{current}, func(m cycleMeasure) map[string]reviewValue { return m.categories }, &full)
	if len(rows) != 1 || rows[0].Amount != "0" || rows[0].Previous != nil || rows[0].Delta != nil || rows[0].Median != nil || *rows[0].PreviousFull != "1950000" || *rows[0].DeltaFull != "-1950000" {
		t.Fatalf("full-only category=%+v", rows)
	}
}

func TestCategoryOrderUsesDisplayedFullCycleDelta(t *testing.T) {
	current, previous, full := newCycleMeasure(reviewPeriod{}), newCycleMeasure(reviewPeriod{}), newCycleMeasure(reviewPeriod{})
	current.categories["rent"] = reviewValue{ID: "rent", Name: "Rent", Amount: "1950000"}
	current.categories["food"] = reviewValue{ID: "food", Name: "Food", Amount: "500000"}
	previous.categories["food"] = current.categories["food"]
	full.categories["rent"] = current.categories["rent"]
	full.categories["food"] = reviewValue{ID: "food", Name: "Food", Amount: "1000000"}
	rows := changes([]cycleMeasure{current, previous}, func(m cycleMeasure) map[string]reviewValue { return m.categories }, &full)
	if len(rows) != 2 || rows[0].ID != "food" || *rows[0].DeltaFull != "-500000" || rows[1].ID != "rent" || *rows[1].DeltaFull != "0" || *rows[1].Delta != "1950000" {
		t.Fatalf("displayed full-cycle order=%+v", rows)
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

func historyPeriods() []reviewPeriod {
	end := func(date string) *string { return &date }
	return []reviewPeriod{ // newest first, as reviewPeriods returns them
		{Kind: "SALARY_CYCLE", Start: "2026-09-01", MeasuredUntil: "2026-09-11", State: "ACTIVE"},
		// An active review rewrites this cycle's cutoff to the equal-day prefix.
		{Kind: "SALARY_CYCLE", Start: "2026-08-01", End: end("2026-09-01"), MeasuredUntil: "2026-08-11", State: "CLOSED"},
		{Kind: "SALARY_CYCLE", Start: "2026-07-01", End: end("2026-08-01"), MeasuredUntil: "2026-08-01", State: "CLOSED"},
		{Kind: "SALARY_CYCLE", Start: "2026-06-01", End: end("2026-07-01"), MeasuredUntil: "2026-07-01", State: "CLOSED"},
	}
}

func windowStarts(window []reviewPeriod) string {
	starts := []string{}
	for _, p := range window {
		starts = append(starts, p.Start)
	}
	return strings.Join(starts, ",")
}

func TestHistoryWindowEndsAtNewestOrAtOlderSelection(t *testing.T) {
	periods := historyPeriods()
	for _, tc := range []struct {
		name        string
		index, size int
		want        string
	}{
		{"newest cycles", 0, 2, "2026-08-01,2026-09-01"},
		{"clipped to available cycles", 0, 6, "2026-06-01,2026-07-01,2026-08-01,2026-09-01"},
		{"selected inside the newest window", 1, 2, "2026-08-01,2026-09-01"},
		{"older selection ends the window", 2, 2, "2026-06-01,2026-07-01"},
		{"oldest selection has no older cycles", 3, 2, "2026-06-01"},
	} {
		if got := windowStarts(historyWindow(periods, tc.index, tc.size)); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
	if historyWindow(periods, 0, 0) != nil || historyWindow(nil, 0, 3) != nil || historyWindow(periods, 9, 3) != nil {
		t.Fatal("empty or invalid selection must not produce a window")
	}
}

func TestHistoryWindowMeasuresClosedCyclesToTheirFullEnd(t *testing.T) {
	window := historyWindow(historyPeriods(), 0, 2)
	if window[0].Start != "2026-08-01" || window[0].MeasuredUntil != "2026-09-01" {
		t.Fatalf("closed cycle kept the equal-day cutoff: %+v", window[0])
	}
	if window[1].State != "ACTIVE" || window[1].MeasuredUntil != "2026-09-11" {
		t.Fatalf("active cycle cutoff changed: %+v", window[1])
	}
}

func TestHistoryMeasuresReuseLoadedCyclesWithTheSameCutoff(t *testing.T) {
	periods := historyPeriods()
	measures := []cycleMeasure{newCycleMeasure(periods[0]), newCycleMeasure(reviewPeriod{Start: "2026-08-01", MeasuredUntil: "2026-08-11"})}
	out, indices := historyMeasures(measures, historyWindow(periods, 0, 3))
	// 07-01 and the full 08-01 cycle are new; the active cycle is reused.
	if len(out) != 4 || len(indices) != 3 || indices[0] != 2 || indices[1] != 3 || indices[2] != 0 {
		t.Fatalf("out=%d indices=%v", len(out), indices)
	}
	if out[3].period.Start != "2026-08-01" || out[3].period.MeasuredUntil != "2026-09-01" {
		t.Fatalf("a prefix measure must not stand in for the full cycle: %+v", out[3].period)
	}
}

func historyMeasure(start string, categories map[string]string) cycleMeasure {
	m := newCycleMeasure(reviewPeriod{Start: start, MeasuredUntil: start, State: "CLOSED"})
	expense := "0"
	for id, amount := range categories {
		m.categories[id] = reviewValue{ID: id, Name: "Name " + id, Amount: amount}
		expense = financialmath.Add(expense, amount)
	}
	m.cash.Expense = expense
	return m
}

func TestBuildHistoryReconcilesEveryCycleExactly(t *testing.T) {
	first, second := map[string]string{}, map[string]string{}
	for i, id := range []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8"} {
		first[id], second[id] = []string{"100", "200", "300", "400", "500", "600", "700", "800"}[i], "10"
	}
	first["p9"], second["p9"] = "50", "5"    // ninth by total, so it falls into Other
	first["r1"], second["r1"] = "-30", "-20" // refund-only category
	first["z"], second["z"] = "0", "0"       // never listed: zero over the window
	measures := []cycleMeasure{historyMeasure("2026-06-01", first), historyMeasure("2026-07-01", second)}
	history, matrix := buildHistory(measures, []int{0, 1})
	if len(history) != 2 || len(matrix.Rows) != 8 || matrix.Rows[0].ID != "p8" || matrix.Rows[7].ID != "p1" {
		t.Fatalf("rows=%+v", matrix.Rows)
	}
	if history[0].ExpenseDelta != nil || history[1].ExpenseDelta == nil || *history[1].ExpenseDelta != "-3555" {
		t.Fatalf("expense delta is server arithmetic against the preceding entry: %+v", history)
	}
	if strings.Join(matrix.CycleStarts, ",") != "2026-06-01,2026-07-01" {
		t.Fatalf("starts=%v", matrix.CycleStarts)
	}
	if got := strings.Join(matrix.Other.Amounts, ","); got != "20,-15" {
		t.Fatalf("other=%s (nonlisted categories net, negative allowed)", got)
	}
	for i := range history {
		listed := "0"
		for _, row := range matrix.Rows {
			listed = financialmath.Add(listed, row.Amounts[i])
		}
		if financialmath.Add(listed, matrix.Other.Amounts[i]) != history[i].Expense {
			t.Fatalf("cycle %d does not reconcile: listed=%s other=%s expense=%s", i, listed, matrix.Other.Amounts[i], history[i].Expense)
		}
	}
}

func TestBuildHistoryOrdersTiesByNameThenIDAndKeepsUncategorized(t *testing.T) {
	m := historyMeasure("2026-06-01", map[string]string{"b": "100", "a": "100", "": "300"})
	m.categories[""] = reviewValue{ID: "", Name: "Belum dikategorikan", Amount: "300"}
	_, matrix := buildHistory([]cycleMeasure{m}, []int{0})
	if len(matrix.Rows) != 3 || matrix.Rows[0].ID != "" || matrix.Rows[0].Name != "Belum dikategorikan" || matrix.Rows[1].ID != "a" || matrix.Rows[2].ID != "b" {
		t.Fatalf("rows=%+v", matrix.Rows)
	}
}

func TestEmptyHistoryEncodesArraysAndKeepsTheContractFieldNames(t *testing.T) {
	history, matrix := buildHistory(nil, nil)
	raw, err := json.Marshal(Facts{History: history, CategoryHistory: matrix})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"history":[]`, `"cycleStarts":[]`, `"rows":[]`, `"other":{"amounts":[]}`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
	end := "2026-07-01"
	one, err := json.Marshal(historyCycle{Start: "2026-06-01", End: &end, MeasuredUntil: end, State: "CLOSED", Income: "1", GrossExpense: "2", Refund: "3", Expense: "4", Net: "5", SavingsAllocated: "6"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"start"`, `"end"`, `"measuredUntil"`, `"state"`, `"income"`, `"grossExpense"`, `"refund"`, `"expense"`, `"netCashflow"`, `"savingsAllocated"`, `"expenseDelta"`} {
		if !strings.Contains(string(one), want) {
			t.Fatalf("history entry lost %s: %s", want, one)
		}
	}
}

func paceMeasure(start, until string, daily map[string]string) cycleMeasure {
	m := newCycleMeasure(reviewPeriod{Start: start, MeasuredUntil: until, State: "CLOSED"})
	for day, amount := range daily {
		m.dailyNet[day] = amount
	}
	return m
}

func TestCumulativeSeriesRunsToTheCutoffAndAllowsRefunds(t *testing.T) {
	m := paceMeasure("2026-06-01", "2026-06-05", map[string]string{"2026-06-01": "100", "2026-06-03": "50", "2026-06-04": "-30"})
	if got := strings.Join(cumulativeSeries(m), ","); got != "100,100,150,120" {
		t.Fatalf("series=%s", got)
	}
	if got := strings.Join(cumulativeSeries(paceMeasure("2026-06-01", "2026-06-04", nil)), ","); got != "0,0,0" {
		t.Fatalf("a cycle with no spending is a flat zero curve, got %s", got)
	}
	if cumulativeSeries(paceMeasure("not-a-date", "2026-06-04", nil)) != nil {
		t.Fatal("an unreadable period must not invent a curve")
	}
}

func TestBuildPaceUsesTheFullPreviousCycleAndTheDaysAllThreeShare(t *testing.T) {
	a := paceMeasure("2026-05-01", "2026-05-06", map[string]string{"2026-05-02": "100", "2026-05-04": "200"}) // 0,100,100,300,300
	b := paceMeasure("2026-06-01", "2026-06-05", map[string]string{"2026-06-01": "500", "2026-06-03": "100"}) // 500,500,600,600
	c := paceMeasure("2026-07-01", "2026-07-08", map[string]string{"2026-07-01": "300", "2026-07-03": "400"}) // 300,300,700,700,700,700,700
	pace := buildPace(&a, []cycleMeasure{a, b, c}, true)
	if got := strings.Join(pace.PreviousFullCycle, ","); got != "0,100,100,300,300" {
		t.Fatalf("previous=%s", got)
	}
	if got := strings.Join(pace.Median3, ","); got != "300,300,600,600" {
		t.Fatalf("median over the 4 shared days = %s", got)
	}
	none := buildPace(nil, nil, false)
	if none.PreviousFullCycle != nil || none.Median3 != nil {
		t.Fatalf("no history means no curves, not zeros: %+v", none)
	}
	if buildPace(nil, []cycleMeasure{a, b}, true).Median3 != nil {
		t.Fatal("a median needs exactly three eligible cycles")
	}
	raw, err := json.Marshal(none)
	if err != nil || string(raw) != `{"previousFullCycle":null,"median3":null}` {
		t.Fatalf("missing curves encode as null: %s %v", raw, err)
	}
}
