package analytics

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/clock"
)

type overviewComparison struct {
	PeriodKind      string  `json:"periodKind"`
	Income          string  `json:"income"`
	Expense         string  `json:"expense"`
	ConfirmedCount  int     `json:"confirmedCount"`
	PreviousExpense *string `json:"previousExpense"`
	PreviousDays    *int    `json:"previousDays"`
	ExpenseDelta    *string `json:"expenseDelta"`
}

func (f reviewFixture) overview(t *testing.T, now time.Time) overviewComparison {
	t.Helper()
	h := NewCycleHandler(f.pool, func() time.Time { return now })
	r := httptest.NewRequest("GET", "/api/v1/analytics/overview", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{HouseholdID: f.household, HasHousehold: true}))
	w := httptest.NewRecorder()
	h.Overview(w, r)
	var out overviewComparison
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	return out
}

func TestOverviewComparesAgainstPreviousSalaryCycle(t *testing.T) {
	t.Parallel()
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-08-24")
	f.anchor(t, "2026-09-25")
	f.transaction(t, "2026-09-24", "EXPENSE", "CONFIRMED", "1000000", true)
	f.transaction(t, "2026-09-20", "REFUND", "CONFIRMED", "100000", true)
	f.transaction(t, "2026-09-26", "EXPENSE", "CONFIRMED", "250000", true)
	f.transaction(t, "2026-09-27", "REFUND", "CONFIRMED", "50000", true)
	f.transaction(t, "2026-09-28", "EXPENSE", "NEEDS_REVIEW", "9999999", true)
	got := f.overview(t, time.Date(2026, 10, 1, 12, 0, 0, 0, clock.HouseholdLocation()))
	if got.PeriodKind != "CURRENT_CYCLE" || got.Expense != "200000" || got.ConfirmedCount != 3 {
		t.Fatalf("overview=%+v", got)
	}
	if got.PreviousExpense == nil || *got.PreviousExpense != "900000" || got.PreviousDays == nil || *got.PreviousDays != 32 || got.ExpenseDelta == nil || *got.ExpenseDelta != "-700000" {
		t.Fatalf("comparison=%+v", got)
	}
}

func TestOverviewFirstCycleHasNoComparison(t *testing.T) {
	t.Parallel()
	f := cycleReviewFixture(t)
	f.anchor(t, "2026-09-25")
	f.transaction(t, "2026-09-26", "EXPENSE", "CONFIRMED", "250000", true)
	got := f.overview(t, time.Date(2026, 10, 1, 12, 0, 0, 0, clock.HouseholdLocation()))
	if got.PreviousExpense != nil || got.PreviousDays != nil || got.ExpenseDelta != nil {
		t.Fatalf("first cycle must report no comparison: %+v", got)
	}
}

func TestOverviewCalendarMonthComparisonAndEmptyHousehold(t *testing.T) {
	t.Parallel()
	f := cycleReviewFixture(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, clock.HouseholdLocation())
	empty := f.overview(t, now)
	if empty.PeriodKind != "CALENDAR_MONTH" || empty.ConfirmedCount != 0 || empty.PreviousExpense != nil || empty.ExpenseDelta != nil {
		t.Fatalf("empty=%+v", empty)
	}
	f.transaction(t, "2026-09-10", "EXPENSE", "CONFIRMED", "400000", true)
	f.transaction(t, "2026-10-05", "EXPENSE", "CONFIRMED", "150000", true)
	got := f.overview(t, now)
	if got.ConfirmedCount != 1 || got.PreviousExpense == nil || *got.PreviousExpense != "400000" || got.PreviousDays == nil || *got.PreviousDays != 30 || got.ExpenseDelta == nil || *got.ExpenseDelta != "-250000" {
		t.Fatalf("calendar=%+v", got)
	}
}

func TestCategoriesCurrentCycleMatchesOverviewWindow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		anchors      []string
		now          time.Time
		wantStart    string
		expenseDates []string
	}{
		{"salary cycle", []string{"2026-08-24", "2026-09-25"}, time.Date(2026, 10, 1, 12, 0, 0, 0, clock.HouseholdLocation()), "2026-09-25", []string{"2026-09-26", "2026-09-24"}},
		{"calendar month", nil, time.Date(2026, 10, 15, 12, 0, 0, 0, clock.HouseholdLocation()), "2026-10-01", []string{"2026-10-05", "2026-09-30"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := cycleReviewFixture(t)
			for _, a := range tc.anchors {
				f.anchor(t, a)
			}
			for _, d := range tc.expenseDates {
				f.transaction(t, d, "EXPENSE", "CONFIRMED", "100000", true)
			}
			h := NewCycleHandler(f.pool, func() time.Time { return tc.now })
			r := httptest.NewRequest("GET", "/api/v1/analytics/categories?period=current_cycle", nil)
			r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{HouseholdID: f.household, HasHousehold: true}))
			w := httptest.NewRecorder()
			h.Categories(w, r)
			var rows []struct{ ID, Name, Amount string }
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0].Amount != "100000" || w.Header().Get("X-Window-Start") != tc.wantStart {
				t.Fatalf("status=%d start=%q body=%s", w.Code, w.Header().Get("X-Window-Start"), w.Body.String())
			}
		})
	}
}

func TestCategoriesLast3CyclesRanksHouseholdAndBreaksTiesDeterministically(t *testing.T) {
	t.Parallel()
	f := cycleReviewFixture(t)
	for _, d := range []string{"2026-06-25", "2026-07-25", "2026-08-25", "2026-09-25"} {
		f.anchor(t, d)
	}
	f.categorized(t, "Old Cycle Only", "2026-07-01", "EXPENSE", "9000000")
	f.categorized(t, "Top One", "2026-07-26", "EXPENSE", "500000")
	f.categorized(t, "Top One", "2026-09-26", "EXPENSE", "400000")
	f.categorized(t, "Tie Beta", "2026-08-26", "EXPENSE", "300000")
	f.categorized(t, "Tie Alpha", "2026-08-26", "EXPENSE", "300000")
	f.categorized(t, "With Refund", "2026-09-26", "EXPENSE", "250000")
	f.categorized(t, "With Refund", "2026-09-27", "REFUND", "100000")

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, clock.HouseholdLocation())
	h := NewCycleHandler(f.pool, func() time.Time { return now })
	r := httptest.NewRequest("GET", "/api/v1/analytics/categories?period=last_3_cycles", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{HouseholdID: f.household, HasHousehold: true}))
	w := httptest.NewRecorder()
	h.Categories(w, r)
	var rows []struct {
		ID     *string `json:"id"`
		Name   string  `json:"name"`
		Amount string  `json:"amount"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rows) != nil {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Window-Kind") != "SALARY_CYCLES" || w.Header().Get("X-Window-Cycles") != "3" || w.Header().Get("X-Window-Start") != "2026-07-25" {
		t.Fatalf("headers kind=%q cycles=%q start=%q", w.Header().Get("X-Window-Kind"), w.Header().Get("X-Window-Cycles"), w.Header().Get("X-Window-Start"))
	}
	if len(rows) != 4 || rows[0].Name != "Top One" || rows[0].Amount != "900000" || rows[3].Name != "With Refund" || rows[3].Amount != "150000" {
		t.Fatalf("rows=%+v", rows)
	}
	if rows[1].ID == nil || rows[2].ID == nil || *rows[1].ID >= *rows[2].ID {
		t.Fatalf("ties must be ordered by category id ascending: %+v", rows[1:3])
	}
}
