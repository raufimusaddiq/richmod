package analyticscore

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/financialmath"
)

func rankingMeasure(expense string, categories map[string]reviewValue) cycleMeasure {
	m := newCycleMeasure(reviewPeriod{})
	m.cash.Expense = expense
	m.categories = categories
	return m
}

func TestCategoryRankingOrdersNumericallyAndReconcilesToExpense(t *testing.T) {
	// String order differs from numeric order: "9000" > "20000" > "100000" as strings.
	cats := map[string]reviewValue{}
	total := "0"
	for i, amount := range []string{"9000", "20000", "100000", "5", "700", "3000", "45000", "800", "60", "1000000"} {
		id := string(rune('a' + i))
		cats[id] = reviewValue{ID: id, Name: "Cat " + id, Amount: amount, Count: 1}
		total = financialmath.Add(total, amount)
	}
	cats[""] = reviewValue{ID: "", Name: "Belum dikategorikan", Amount: "250", Count: 2}
	total = financialmath.Add(total, "250")
	got := buildCategoryRanking(rankingMeasure(total, cats), nil, "FULL_CYCLE")
	if len(got.Rows) != 7 || got.Baseline != "PREVIOUS_FULL" {
		t.Fatalf("rows=%d baseline=%s", len(got.Rows), got.Baseline)
	}
	want := []string{"1000000", "100000", "45000", "20000", "9000", "3000", "800"}
	sum := "0"
	for i, r := range got.Rows {
		if r.Amount != want[i] {
			t.Fatalf("row %d = %s want %s", i, r.Amount, want[i])
		}
		if r.Baseline != nil || r.Delta != nil || r.Relative != nil {
			t.Fatalf("no baseline measure must give null deltas: %+v", r)
		}
		sum = financialmath.Add(sum, r.Amount)
	}
	if financialmath.Add(sum, got.Other.Amount) != total {
		t.Fatalf("rows+other=%s total=%s", financialmath.Add(sum, got.Other.Amount), total)
	}
	if got.Other.CategoryCount != 4 || got.Other.Count != 5 || got.Other.Name != "Kategori lain" || got.Other.Delta != nil {
		t.Fatalf("other=%+v", got.Other)
	}
}

func TestCategoryRankingFewCategoriesUncategorizedAndBaseline(t *testing.T) {
	cur := rankingMeasure("1300", map[string]reviewValue{
		"food": {ID: "food", Name: "Food", Amount: "1000", Count: 3},
		"":     {ID: "", Name: "Belum dikategorikan", Amount: "300", Count: 1},
	})
	base := rankingMeasure("1500", map[string]reviewValue{
		"food":  {ID: "food", Name: "Food", Amount: "1500"},
		"other": {ID: "other", Name: "Gone", Amount: "0"},
	})
	got := buildCategoryRanking(cur, &base, "ELAPSED_DAYS")
	if got.Baseline != "SAME_DAY" || len(got.Rows) != 2 || got.Rows[0].ID == nil || *got.Rows[0].ID != "food" {
		t.Fatalf("ranking=%+v", got)
	}
	if got.Rows[1].ID != nil || got.Rows[1].Name != "Belum dikategorikan" {
		t.Fatalf("uncategorized row must carry a null id: %+v", got.Rows[1])
	}
	if *got.Rows[0].Delta != "-500" || *got.Rows[0].Relative != "-0.3333" || *got.Rows[1].Baseline != "0" || got.Rows[1].Relative != nil {
		t.Fatalf("deltas=%+v %+v", got.Rows[0], got.Rows[1])
	}
	if got.Other.Amount != "0" || got.Other.CategoryCount != 0 || *got.Other.Baseline != "0" || *got.Other.Delta != "0" {
		t.Fatalf("remainder bucket: %+v", got.Other)
	}
	empty := buildCategoryRanking(rankingMeasure("0", map[string]reviewValue{}), nil, "FULL_CYCLE")
	raw, err := json.Marshal(empty)
	if err != nil || !strings.Contains(string(raw), `"rows":[]`) {
		t.Fatalf("empty ranking must encode rows as []: %s %v", raw, err)
	}
}

func TestRoundedPerDayIsHalfAwayFromZeroWholeRupiah(t *testing.T) {
	for _, c := range []struct {
		amount string
		days   int
		want   string
	}{{"7", 2, "4"}, {"5", 2, "3"}, {"1000", 3, "333"}, {"2000", 3, "667"}, {"0", 5, "0"}, {"10", 0, "0"}, {"-5", 2, "-3"}} {
		if got := roundedPerDay(c.amount, c.days); got != c.want {
			t.Fatalf("%s/%d = %s want %s", c.amount, c.days, got, c.want)
		}
	}
}
