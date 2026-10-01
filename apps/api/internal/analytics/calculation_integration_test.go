package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

func TestAnalyticsCycleRangeBucketsAndRefundConsistency(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-08-24")
	f.anchor(t, "2026-09-25")
	startExpense := f.transaction(t, "2026-09-25", "EXPENSE", "CONFIRMED", "250000", true)
	if _, err := f.pool.Exec(context.Background(), `UPDATE transaction SET transaction_at='2026-09-25T00:00:00+07:00' WHERE id=$1`, startExpense); err != nil {
		t.Fatal(err)
	}
	f.transaction(t, "2026-09-26", "REFUND", "CONFIRMED", "50000", true)
	f.transaction(t, "2026-10-01", "EXPENSE", "CONFIRMED", "300", true)
	f.transaction(t, "2026-09-24", "EXPENSE", "CONFIRMED", "1000000", true)
	f.transaction(t, "2026-10-02", "EXPENSE", "CONFIRMED", "9999999", true)
	f.transaction(t, "2026-09-26", "EXPENSE", "NEEDS_REVIEW", "9999999", true)
	h := NewCycleHandler(f.pool, func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, clock.HouseholdLocation()) })
	r := httptest.NewRequest("GET", "/?period=current_cycle", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{HouseholdID: f.household, HasHousehold: true}))
	start, end, err := h.analyticsRange(f.household, r)
	if err != nil || start.Format(time.RFC3339) != "2026-09-25T00:00:00+07:00" || end.Format(time.RFC3339) != "2026-10-02T00:00:00+07:00" {
		t.Fatalf("range=%v..%v err=%v", start, end, err)
	}
	w := httptest.NewRecorder()
	h.Cashflow(w, r)
	var monthly []monthlyValue
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &monthly) != nil || len(monthly) != 2 || monthly[0].Income != "10000000" || monthly[0].Expense != "200000" || monthly[1].Expense != "300" {
		t.Fatalf("cashflow=%s", w.Body.String())
	}
	w = httptest.NewRecorder()
	h.Spending(w, r)
	var spending []map[string]string
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &spending) != nil || len(spending) != 2 || spending[0]["netSpending"] != "200000" || spending[1]["netSpending"] != "300" {
		t.Fatalf("spending=%s", w.Body.String())
	}
	w = httptest.NewRecorder()
	h.CycleDaily(w, r)
	var daily struct {
		Spent, Remaining string
		Daily            []map[string]string
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &daily) != nil || daily.Spent != "200300" || daily.Remaining != "9799700" || daily.Daily[len(daily.Daily)-1]["cumulativeExpense"] != "200300" || daily.Daily[1]["expense"] != "-50000" {
		t.Fatalf("daily=%s", w.Body.String())
	}
	// A 32-day historical cycle must retain its final partial calendar month.
	previousStart, _ := time.ParseInLocation("2006-01-02", "2026-08-24", clock.HouseholdLocation())
	previous, err := h.monthly(r.Context(), f.household, previousStart, start)
	if err != nil || len(previous) != 2 || previous[1].Expense != "1000000" {
		t.Fatalf("previous=%+v err=%v", previous, err)
	}
}

func TestMerchantSharesUseAllNetExpenseBeforeTopTen(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-09-01")
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `INSERT INTO merchant(household_id,normalized_name) SELECT $1,'Fixture merchant '||n FROM generate_series(1,11) n`, f.household); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,merchant_id,confirmed_at) SELECT $1,'EXPENSE','CONFIRMED',100,'2026-09-01T00:00:00+07:00',id,now() FROM merchant WHERE household_id=$1 AND normalized_name LIKE 'Fixture merchant %'`, f.household); err != nil {
		t.Fatal(err)
	}
	f.transaction(t, "2026-09-02", "REFUND", "CONFIRMED", "50", true)
	h := NewCycleHandler(f.pool, func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, clock.HouseholdLocation()) })
	r := httptest.NewRequest("GET", "/?period=current_cycle", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{HouseholdID: f.household, HasHousehold: true}))
	w := httptest.NewRecorder()
	h.Merchants(w, r)
	var merchants []rankedValue
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &merchants) != nil || len(merchants) != 10 {
		t.Fatalf("merchants=%s", w.Body.String())
	}
	for _, m := range merchants {
		if m.Amount != "100" || m.Share != "0.0952" {
			t.Fatalf("share must use 1050 net expense, not 1000 displayed subtotal: %+v", m)
		}
	}
}

// An elapsed active cycle compares only the measured prefix of each earlier
// cycle; the full-cycle window is never pulled forward into the baseline.
func TestCycleReviewBaselineUsesMeasuredPrefixOnly(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-08-01")
	f.anchor(t, "2026-09-01")
	f.transaction(t, "2026-08-15", "EXPENSE", "CONFIRMED", "2000000", true)
	f.transaction(t, "2026-08-16", "REFUND", "CONFIRMED", "50000", true)
	f.transaction(t, "2026-09-08", "EXPENSE", "CONFIRMED", "1950000", true)
	facts := f.review(t, "")
	c := facts.Categories[0]
	if facts.Comparison.Mode != "ELAPSED_DAYS" || facts.Comparison.EligibleCycles != 1 || facts.Comparison.Median3Available || facts.Comparison.Previous.MeasuredUntil != "2026-08-11" {
		t.Fatalf("comparison=%+v", facts.Comparison)
	}
	if *c.Previous != "0" || c.Relative != nil {
		t.Fatalf("rent=%+v", c)
	}
	// Closed selection compares against the previous full closed cycle.
	f.anchor(t, "2026-07-01")
	f.transaction(t, "2026-07-15", "EXPENSE", "CONFIRMED", "1950000", true)
	closed := f.review(t, "?cycle_start=2026-08-01")
	if closed.Comparison.Mode != "FULL_CYCLE" || *closed.Comparison.Expense.Previous != "1950000" || *closed.Comparison.Expense.Delta != "0" {
		t.Fatalf("closed comparison=%+v", closed.Comparison)
	}
}

// A prior cycle shorter than the measured prefix is not a comparable elapsed
// window, so it is excluded from the baseline entirely.
func TestShorterPriorCycleIsNotAnElapsedBaseline(t *testing.T) {
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-08-28")
	f.anchor(t, "2026-09-01")
	f.transaction(t, "2026-08-30", "EXPENSE", "CONFIRMED", "1950000", true)
	facts := f.review(t, "")
	if facts.Comparison.EligibleCycles != 0 || facts.Comparison.Previous != nil || facts.Comparison.Expense.Previous != nil || facts.Comparison.Expense.Median != nil {
		t.Fatalf("ineligible history=%+v", facts.Comparison)
	}
	if len(facts.Categories) != 0 {
		t.Fatalf("current-only category=%+v", facts.Categories)
	}
}

func TestLegacyRankingsDoNotExposeForeignCategoryOrMember(t *testing.T) {
	f := cycleReviewFixture(t)
	foreign := cycleReviewFixture(t)
	f.anchor(t, "2026-09-01")
	id := f.transaction(t, "2026-09-02", "EXPENSE", "CONFIRMED", "100", false)
	if _, err := f.pool.Exec(context.Background(), `UPDATE transaction SET category_id=$2,created_by_user_id=$3 WHERE id=$1`, id, foreign.category, foreign.user); err != nil {
		t.Fatal(err)
	}
	h := NewCycleHandler(f.pool, func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, clock.HouseholdLocation()) })
	r := httptest.NewRequest("GET", "/?period=current_cycle", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{HouseholdID: f.household, HasHousehold: true}))
	for _, handler := range []func(http.ResponseWriter, *http.Request){h.Categories, h.Members} {
		w := httptest.NewRecorder()
		handler(w, r)
		var rows []rankedValue
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0].ID != nil || rows[0].Amount != "100" {
			t.Fatalf("foreign binding exposed: %d %s", w.Code, w.Body.String())
		}
	}
}
