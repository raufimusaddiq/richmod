package analyticscore

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

func sp(value string) *string { return &value }

func fullCycleFacts() Facts {
	var f Facts
	f.Period = reviewPeriod{Kind: "SALARY_CYCLE", Start: "2026-10-25", State: "CLOSED", MeasuredUntil: "2026-11-24"}
	f.Cashflow.Expense = "10000000"
	f.CategoryRanking.Rows = []rankedCategory{
		{Name: "Orang Tua", Amount: "3000000"},
		{Name: "Tempat Tinggal", Amount: "2000000"},
		{Name: "Makanan", Amount: "1500000"},
	}
	f.History = []historyCycle{
		{Start: "2026-08-25", State: "CLOSED", MeasuredUntil: "2026-09-24", Expense: "8620000"},
		{Start: "2026-09-25", State: "CLOSED", MeasuredUntil: "2026-10-25", Expense: "10960000"},
		{Start: "2026-10-25", State: "CLOSED", MeasuredUntil: "2026-11-24", Expense: "9920000"},
	}
	f.Comparison.Previous = &reviewPeriod{Start: "2026-09-25", MeasuredUntil: "2026-10-25"}
	f.Comparison.PreviousFullCycle = f.Comparison.Previous
	f.Comparison.Expense.Previous = sp("10960000")
	f.Categories = []reviewCategory{
		{reviewChange: reviewChange{reviewValue: reviewValue{Name: "Perjalanan"}, DeltaFull: sp("900000")}},
		{reviewChange: reviewChange{reviewValue: reviewValue{Name: "Makanan"}, DeltaFull: sp("100000")}},
		{reviewChange: reviewChange{reviewValue: reviewValue{Name: "Tagihan & Cicilan"}, DeltaFull: sp("-700000")}},
	}
	f.Daily = []map[string]string{{"cumulativeExpense": "9920000"}}
	f.SpendingShape.Days = 31
	return f
}

func TestTakeawaysFullCycle(t *testing.T) {
	got := buildTakeaways(fullCycleFacts())
	want := reviewTakeaways{
		Where:   "Orang Tua dan Tempat Tinggal menghabiskan 50% pengeluaran siklus ini.",
		History: "Rp 8,62 jt → Rp 10,96 jt → Rp 9,92 jt (ditutup)",
		Changes: "Naik paling banyak Perjalanan, turun paling banyak Tagihan & Cicilan.",
		Pace:    "Siklus ini Rp 9,92 jt (31 hari) · siklus lalu Rp 10,96 jt (30 hari).",
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestTakeawaysActiveCycle(t *testing.T) {
	f := fullCycleFacts()
	f.Period.State = "ACTIVE"
	f.Period.MeasuredUntil = "2026-11-24"
	f.History[2].State = "ACTIVE"
	f.History[2].MeasuredUntil = "2026-11-24"
	f.Period.Start = "2026-10-25"
	f.SpendingShape.Days = 30
	f.Comparison.Previous = &reviewPeriod{Start: "2026-09-25", MeasuredUntil: "2026-10-23"}
	for i := range f.Categories {
		f.Categories[i].Delta = f.Categories[i].DeltaFull
		f.Categories[i].DeltaFull = nil
	}
	got := buildTakeaways(f)
	if got.History != "Rp 8,62 jt → Rp 10,96 jt → Rp 9,92 jt (berjalan, hari ke-30)" {
		t.Fatalf("history=%q", got.History)
	}
	if got.Pace != "Hari ke-30: Rp 9,92 jt · siklus lalu Rp 10,96 jt (28 hari)." {
		t.Fatalf("pace=%q", got.Pace)
	}
	if got.Changes != "Naik paling banyak Perjalanan, turun paling banyak Tagihan & Cicilan." {
		t.Fatalf("changes=%q", got.Changes)
	}
}

func TestTakeawaysSingleCategory(t *testing.T) {
	f := fullCycleFacts()
	f.Cashflow.Expense = "1500000"
	f.CategoryRanking.Rows = []rankedCategory{{Name: "Makanan", Amount: "1500000"}}
	if got := buildTakeaways(f).Where; got != "Makanan menghabiskan 100% pengeluaran siklus ini." {
		t.Fatalf("where=%q", got)
	}
}

func TestWhereTakeawayNamesOneOnlyWhenTheTopDominatesOrTheSecondIsTiny(t *testing.T) {
	f := fullCycleFacts()
	f.CategoryRanking.Rows = []rankedCategory{
		{Name: "Orang Tua", Amount: "5000000"},
		{Name: "Tempat Tinggal", Amount: "2000000"},
	}
	if got := whereTakeaway(f); got != "Orang Tua menghabiskan 50% pengeluaran siklus ini." {
		t.Fatalf("dominant where=%q", got)
	}
	f.CategoryRanking.Rows[0].Amount = "2000000"
	f.CategoryRanking.Rows[1].Amount = "500000"
	if got := whereTakeaway(f); got != "Orang Tua menghabiskan 20% pengeluaran siklus ini." {
		t.Fatalf("negligible second where=%q", got)
	}
	f.CategoryRanking.Rows[1].Amount = "2000000"
	if got := whereTakeaway(f); got != "Orang Tua dan Tempat Tinggal menghabiskan 40% pengeluaran siklus ini." {
		t.Fatalf("balanced where=%q", got)
	}
}

func TestTakeawaysVerdictUsesTheShownExpenseDelta(t *testing.T) {
	f := fullCycleFacts()
	f.Period.State = "ACTIVE"
	f.Period.Start, f.Period.MeasuredUntil = "2026-10-25", "2026-11-06"
	f.Comparison.Expense.Delta = sp("-617700")
	if got := buildTakeaways(f).Verdict; got != "Rp 617.700 lebih rendah dari siklus lalu di hari ke-12." {
		t.Fatalf("active verdict=%q", got)
	}
	f.Period.State = "CLOSED"
	f.Comparison.Expense.DeltaFull = sp("1090000")
	if got := buildTakeaways(f).Verdict; got != "Rp 1.090.000 lebih tinggi dari siklus lalu (penuh)." {
		t.Fatalf("closed verdict=%q", got)
	}
	g := fullCycleFacts()
	g.Comparison.Expense = reviewChange{}
	if got := buildTakeaways(g).Verdict; got != "" {
		t.Fatalf("no baseline verdict=%q", got)
	}
}

func TestTakeawaysEmptyCycleOmitsEverything(t *testing.T) {
	var f Facts
	f.Period = reviewPeriod{Kind: "SALARY_CYCLE", Start: "2026-10-25", State: "ACTIVE"}
	f.Cashflow.Expense = "0"
	if got := buildTakeaways(f); got != (reviewTakeaways{}) {
		t.Fatalf("expected no takeaways, got %+v", got)
	}
	encoded, err := json.Marshal(f.Takeaways)
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}

func TestTakeawaysNoBaselineOmitsChangesAndPace(t *testing.T) {
	f := fullCycleFacts()
	f.Comparison.Previous, f.Comparison.PreviousFullCycle, f.Comparison.Expense.Previous = nil, nil, nil
	f.History = f.History[2:]
	got := buildTakeaways(f)
	if got.Changes != "" || got.Pace != "" || got.History != "" {
		t.Fatalf("baseline-less takeaways must be omitted: %+v", got)
	}
	if got.Where == "" {
		t.Fatal("where does not need a baseline")
	}
}

func TestTakeawaysChangesNeutralAndSingleSided(t *testing.T) {
	f := fullCycleFacts()
	f.Categories = f.Categories[:2]
	got := buildTakeaways(f).Changes
	if got != "Naik paling banyak Perjalanan." {
		t.Fatalf("changes=%q", got)
	}
	for _, word := range []string{"boros", "hemat"} {
		if strings.Contains(buildTakeaways(fullCycleFacts()).Changes, word) {
			t.Fatalf("takeaway must stay neutral: %s", word)
		}
	}
}

func TestMillionsTextUsesIntegerRounding(t *testing.T) {
	cases := map[string]string{"8620000": "Rp 8,62 jt", "9925000": "Rp 9,93 jt", "850000": "Rp 850.000", "1000000": "Rp 1,00 jt", "0": "Rp 0", "-1500000": "-Rp 1,50 jt", "1000000000": "Rp 1.000,00 jt", "1234567890": "Rp 1.234,57 jt"}
	for in, want := range cases {
		if got := millionsText(in); got != want {
			t.Errorf("millionsText(%s)=%q want %q", in, got, want)
		}
	}
	if percentText(bigInt(1), bigInt(3)) != "33%" || percentText(bigInt(2), bigInt(3)) != "67%" {
		t.Fatal("percent rounding")
	}
}

func bigInt(n int64) *big.Int { return big.NewInt(n) }
