package analytics

import (
	"context"
	"math/big"
	"testing"
)

func (f reviewFixture) categorized(t *testing.T, name, date, typ, amount string) {
	t.Helper()
	ctx := context.Background()
	var category *string
	if name != "" {
		var id string
		if err := f.pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,$2,lower(replace($2,' ','-'))) ON CONFLICT (household_id,slug) WHERE parent_id IS NULL DO UPDATE SET name=EXCLUDED.name RETURNING id`, f.household, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		category = &id
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,category_id,created_by_user_id,purpose) VALUES($1,$2,'CONFIRMED',$3::numeric,($4::date::timestamp AT TIME ZONE 'Asia/Jakarta')+interval '10 hours',now(),$5,$6,'GENERAL')`, f.household, typ, amount, date, category, f.user); err != nil {
		t.Fatal(err)
	}
}

func TestCycleReviewCategoryRankingTop6PlusOtherReconcilesWithSameDayBaseline(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-08-01")
	f.anchor(t, "2026-09-01")
	for name, amount := range map[string]string{"Cat A": "900000", "Cat B": "1000000", "Cat C": "20000000", "Cat D": "3500", "Cat E": "150000", "Cat F": "75000", "Cat G": "2000000", "Cat H": "40000", "Cat I": "500"} {
		f.categorized(t, name, "2026-09-03", "EXPENSE", amount)
	}
	f.categorized(t, "Cat A", "2026-09-04", "REFUND", "100000")
	f.categorized(t, "", "2026-09-05", "EXPENSE", "7777")
	f.categorized(t, "Cat A", "2026-08-03", "EXPENSE", "300000")
	f.categorized(t, "Cat C", "2026-08-03", "EXPENSE", "5000000")
	f.categorized(t, "Cat D", "2026-08-04", "EXPENSE", "100")
	f.categorized(t, "Cat C", "2026-08-20", "EXPENSE", "9000000")

	facts := f.review(t, "?cycle_start=2026-09-01")
	r := facts.CategoryRanking
	if facts.Comparison.Mode != "ELAPSED_DAYS" || r.Baseline != "SAME_DAY" {
		t.Fatalf("mode=%s baseline=%s", facts.Comparison.Mode, r.Baseline)
	}
	if facts.Cashflow.Expense != "24076777" {
		t.Fatalf("expense=%s", facts.Cashflow.Expense)
	}
	wantNames := []string{"Cat C", "Cat G", "Cat B", "Cat A", "Cat E", "Cat F"}
	wantAmounts := []string{"20000000", "2000000", "1000000", "800000", "150000", "75000"}
	if len(r.Rows) != 6 {
		t.Fatalf("rows=%d", len(r.Rows))
	}
	sum := new(big.Int)
	for i, row := range r.Rows {
		if row.Name != wantNames[i] || row.Amount != wantAmounts[i] || row.ID == nil {
			t.Fatalf("row %d = %+v", i, row)
		}
		n, _ := new(big.Int).SetString(row.Amount, 10)
		sum.Add(sum, n)
	}
	other, _ := new(big.Int).SetString(r.Other.Amount, 10)
	if sum.Add(sum, other).String() != facts.Cashflow.Expense {
		t.Fatalf("rows+other=%s expense=%s", sum, facts.Cashflow.Expense)
	}
	if r.Other.Name != "Kategori lain" || r.Other.Amount != "51777" || r.Other.CategoryCount != 4 || len(r.Other.Rows) != 4 {
		t.Fatalf("other=%+v", r.Other)
	}
	wantOtherNames := []string{"Cat H", "Belum dikategorikan", "Cat D", "Cat I"}
	wantOtherAmounts := []string{"40000", "7777", "3500", "500"}
	for i, row := range r.Other.Rows {
		if row.Name != wantOtherNames[i] || row.Amount != wantOtherAmounts[i] {
			t.Fatalf("other row %d = %+v", i, row)
		}
	}
	if c := r.Rows[0]; c.Baseline == nil || *c.Baseline != "5000000" || *c.Delta != "15000000" {
		t.Fatalf("same-day baseline must exclude the later 9,000,000: %+v", c)
	}
	if g := r.Rows[1]; *g.Baseline != "0" || *g.Delta != "2000000" || g.Relative != nil {
		t.Fatalf("new category: %+v", g)
	}
	if r.Other.Baseline == nil || *r.Other.Baseline != "100" || *r.Other.Delta != "51677" {
		t.Fatalf("other baseline=%+v", r.Other)
	}
	if facts.SpendingShape.AverageRounded != "2407678" {
		t.Fatalf("rounded average=%s (raw %s, days %d)", facts.SpendingShape.AverageRounded, facts.SpendingShape.Average, facts.SpendingShape.Days)
	}
}

func TestCycleReviewCategoryRankingUsesThreeCycleTopSixOverCurrentSpike(t *testing.T) {
	f := cycleReviewFixture(t)
	for _, d := range []string{"2026-07-01", "2026-08-01", "2026-09-01"} {
		f.anchor(t, d)
	}
	for _, name := range []string{"Top 1", "Top 2", "Top 3", "Top 4", "Top 5", "Top 6"} {
		f.categorized(t, name, "2026-07-05", "EXPENSE", "1000000")
		f.categorized(t, name, "2026-08-05", "EXPENSE", "1000000")
		f.categorized(t, name, "2026-09-03", "EXPENSE", "100000")
	}
	f.categorized(t, "Perjalanan", "2026-09-04", "EXPENSE", "631000")

	facts := f.review(t, "?cycle_start=2026-09-01")
	r := facts.CategoryRanking
	if len(r.Rows) != 6 {
		t.Fatalf("expected 6 ranked rows, got %d: %+v", len(r.Rows), r.Rows)
	}
	for _, row := range r.Rows {
		if row.Name == "Perjalanan" {
			t.Fatalf("non-top-6 category Perjalanan must fold into Kategori lain: %+v", r.Rows)
		}
	}
	if r.Other.Amount != "631000" || r.Other.CategoryCount != 1 || len(r.Other.Rows) != 1 || r.Other.Rows[0].Name != "Perjalanan" {
		t.Fatalf("expected Perjalanan inside other.rows, got %+v", r.Other)
	}
}

func TestCycleReviewCategoryRankingFewCategoriesAndUncategorizedOnClosedCycle(t *testing.T) {
	f := cycleReviewFixture(t)
	for _, d := range []string{"2026-07-01", "2026-08-01", "2026-09-01"} {
		f.anchor(t, d)
	}
	f.categorized(t, "Cat X", "2026-07-05", "EXPENSE", "400000")
	f.categorized(t, "Cat X", "2026-08-05", "EXPENSE", "1000000")
	f.categorized(t, "Cat Y", "2026-08-06", "EXPENSE", "250000")
	f.categorized(t, "", "2026-08-07", "EXPENSE", "5000")

	facts := f.review(t, "?cycle_start=2026-08-01")
	r := facts.CategoryRanking
	if r.Baseline != "PREVIOUS_FULL" || len(r.Rows) != 2 || facts.Cashflow.Expense != "1255000" {
		t.Fatalf("ranking=%+v expense=%s", r, facts.Cashflow.Expense)
	}
	if r.Rows[0].Name != "Cat X" || r.Rows[1].Name != "Cat Y" {
		t.Fatalf("rows=%+v", r.Rows)
	}
	if *r.Rows[0].Baseline != "400000" || *r.Rows[0].Delta != "600000" || *r.Rows[0].Relative != "1.5000" {
		t.Fatalf("baseline=%+v", r.Rows[0])
	}
	if r.Other.Amount != "5000" || r.Other.CategoryCount != 1 || len(r.Other.Rows) != 1 || r.Other.Rows[0].ID != nil || r.Other.Rows[0].Name != "Belum dikategorikan" || *r.Other.Baseline != "0" || *r.Other.Delta != "5000" {
		t.Fatalf("uncategorized remainder bucket=%+v", r.Other)
	}
	first := f.review(t, "?cycle_start=2026-07-01").CategoryRanking
	if len(first.Rows) != 1 || first.Rows[0].Delta != nil || first.Other.Baseline != nil {
		t.Fatalf("first cycle=%+v", first)
	}
}
