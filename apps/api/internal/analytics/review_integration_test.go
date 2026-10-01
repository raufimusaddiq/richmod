package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

type reviewFixture struct {
	pool                                                 *pgxpool.Pool
	household, user, source, category, merchant, account string
}

func cycleReviewFixture(t *testing.T) reviewFixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := reviewFixture{pool: pool}
	stamp := time.Now().UnixNano()
	insert := func(target *string, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	insert(&f.household, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Cycle review %d", stamp))
	insert(&f.user, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Review member','unused') RETURNING id`, fmt.Sprintf("cycle-review-%d@example.test", stamp))
	insert(&f.source, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Fixture salary','fixture salary',true) RETURNING id`, f.household, f.user)
	insert(&f.category, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','fixture-dining') RETURNING id`, f.household)
	insert(&f.merchant, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Fixture cafe') RETURNING id`, f.household)
	insert(&f.account, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Fixture savings','ASSET','BANK','SAVINGS') RETURNING id`, f.household)
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, f.household, f.user); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f reviewFixture) anchor(t *testing.T, date string) {
	t.Helper()
	ctx := context.Background()
	var event, transaction string
	if err := f.pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'SYSTEM',$2::text,($2::text::date::timestamp AT TIME ZONE 'Asia/Jakarta'),decode(md5($2::text),'hex'),'PROCESSED') RETURNING id`, f.household, date).Scan(&event); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,created_by_user_id) VALUES($1,'INCOME','CONFIRMED',10000000,($2::date::timestamp AT TIME ZONE 'Asia/Jakarta'),now(),$3) RETURNING id`, f.household, date, f.user).Scan(&transaction); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO salary_event(household_id,salary_source_id,payroll_period,pay_date,net_pay,transaction_id,source_event_id,status) VALUES($1,$2,$3::date,$3::date,10000000,$4,$5,'CONFIRMED')`, f.household, f.source, date, transaction, event); err != nil {
		t.Fatal(err)
	}
}

func (f reviewFixture) transaction(t *testing.T, date, typ, status, amount string, categorized bool) string {
	t.Helper()
	var id string
	var category, merchant, account *string
	if categorized {
		category = &f.category
		merchant = &f.merchant
	}
	purpose := "GENERAL"
	if typ == "TRANSFER" {
		purpose = "SAVINGS_TRANSFER"
		account = &f.account
	}
	err := f.pool.QueryRow(context.Background(), `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,category_id,merchant_id,created_by_user_id,purpose,related_wealth_account_id) VALUES($1,$2,$3,$4::numeric,($5::date::timestamp AT TIME ZONE 'Asia/Jakarta')+interval '10 hours',CASE WHEN $3='CONFIRMED' THEN now() ELSE NULL END,$6,$7,$8,$9,$10) RETURNING id`, f.household, typ, status, amount, date, category, merchant, f.user, purpose, account).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f reviewFixture) review(t *testing.T, query string) cycleReview {
	t.Helper()
	h := NewCycleHandler(f.pool, func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, clock.HouseholdLocation()) })
	r := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cycle-review"+query, nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: f.user, Memberships: []auth.Membership{{HouseholdID: f.household, Role: "OWNER"}}}))
	w := httptest.NewRecorder()
	h.CycleReview(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("financial facts must not be cached as immutable closed-cycle data")
	}
	var facts cycleReview
	if err := json.Unmarshal(w.Body.Bytes(), &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestCycleReviewRefundBaselinesDriversWealthAndIsolation(t *testing.T) {
	f := cycleReviewFixture(t)
	output := captureProductEvents(t)
	for _, date := range []string{"2026-05-01", "2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01"} {
		f.anchor(t, date)
	}
	for i, amount := range []string{"1350000", "1500000", "700000", "1450000"} {
		f.transaction(t, fmt.Sprintf("2026-%02d-05", i+5), "EXPENSE", "CONFIRMED", amount, true)
	}
	f.transaction(t, "2026-08-06", "REFUND", "CONFIRMED", "50000", true)
	f.transaction(t, "2026-08-07", "TRANSFER", "CONFIRMED", "2000000", false)
	unresolved := f.transaction(t, "2026-08-08", "EXPENSE", "NEEDS_REVIEW", "7777777", true)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO review_item(household_id,transaction_id,review_type,status) VALUES($1,$2,'UNKNOWN_MERCHANT','OPEN')`, f.household, unresolved); err != nil {
		t.Fatal(err)
	}
	f.transaction(t, "2026-09-01", "EXPENSE", "CONFIRMED", "9999999", true)
	f.transaction(t, "2026-08-31", "EXPENSE", "PENDING", "8888888", true)
	for i, item := range []struct{ at, value string }{{"2026-07-31T23:00:00+07:00", "20000000"}, {"2026-08-31T23:00:00+07:00", "30000000"}, {"2026-09-05T10:00:00+07:00", "99999999"}} {
		var snapshot string
		if err := f.pool.QueryRow(context.Background(), `INSERT INTO wealth_snapshot(household_id,observed_at,created_by_user_id) VALUES($1,$2::timestamptz,$3) RETURNING id`, f.household, item.at, f.user).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(context.Background(), `INSERT INTO wealth_snapshot_item(snapshot_id,wealth_account_id,value_idr,source) VALUES($1,$2,$3::numeric,'MANUAL')`, snapshot, f.account, item.value); err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
	}
	facts := f.review(t, "?cycle_start=2026-08-01")
	if facts.Period.State != "CLOSED" || *facts.Period.End != "2026-09-01" || len(facts.Daily) != 31 {
		t.Fatalf("period=%+v days=%d", facts.Period, len(facts.Daily))
	}
	if facts.Cashflow.Expense != "1400000" || facts.Cashflow.Income != "10000000" || facts.Cashflow.Net != "8600000" || facts.Cashflow.Allocated != "2000000" || facts.Cashflow.Unallocated != "6600000" {
		t.Fatalf("cashflow=%+v", facts.Cashflow)
	}
	if !facts.Comparison.Median3Available || *facts.Comparison.Expense.Previous != "700000" || *facts.Comparison.Expense.Median != "1350000" {
		t.Fatalf("comparison=%+v", facts.Comparison)
	}
	if len(facts.Categories) != 1 || *facts.Categories[0].Contribution != "1.0000" || len(facts.Categories[0].Merchants) != 1 || len(facts.Categories[0].Transactions) != 2 {
		t.Fatalf("categories=%+v", facts.Categories)
	}
	if len(facts.Destinations) != 1 || facts.Destinations[0].Amount != "2000000" || len(facts.Members) != 1 {
		t.Fatalf("destinations=%+v members=%+v", facts.Destinations, facts.Members)
	}
	if facts.Wealth.Current.NetWorth != "30000000" || facts.Wealth.Previous.NetWorth != "20000000" || *facts.Wealth.Change != "10000000" || *facts.Wealth.Cashflow != "8600000" || *facts.Wealth.Other != "1400000" {
		t.Fatalf("wealth=%+v", facts.Wealth)
	}
	if len(facts.Quality) != 1 || facts.Quality[0].Kind != "OPEN_REVIEWS" || facts.Quality[0].Count != 2 {
		t.Fatalf("quality=%+v", facts.Quality)
	}
	other := cycleReviewFixture(t)
	other.anchor(t, "2026-08-01")
	other.anchor(t, "2026-09-01")
	other.transaction(t, "2026-08-05", "EXPENSE", "CONFIRMED", "44444444", true)
	isolated := f.review(t, "?cycle_start=2026-08-01")
	if isolated.Cashflow != facts.Cashflow || isolated.Wealth.Current.ID != facts.Wealth.Current.ID {
		t.Fatal("another household changed financial facts")
	}
	assertProductEvents(t, output, "CYCLE_REVIEW_OPENED", 2, "version", "period_kind", "period_state")
}

func TestCycleReviewHistoryAvailabilityAndActiveElapsedDays(t *testing.T) {
	f := cycleReviewFixture(t)
	fallback := f.review(t, "")
	if fallback.Period.Kind != "CALENDAR_MONTH" || fallback.Comparison.Previous != nil || fallback.Comparison.Median3Available {
		t.Fatalf("fallback=%+v", fallback)
	}
	f.anchor(t, "2026-08-01")
	noHistory := f.review(t, "")
	if noHistory.Comparison.EligibleCycles != 0 || noHistory.Comparison.Expense.Previous != nil {
		t.Fatalf("history=%+v", noHistory.Comparison)
	}
	f.anchor(t, "2026-09-01")
	f.anchor(t, "2026-10-01") // future salary cannot close the active cycle
	f.transaction(t, "2026-08-05", "EXPENSE", "CONFIRMED", "1000", true)
	f.transaction(t, "2026-08-20", "EXPENSE", "CONFIRMED", "9000", true)
	f.transaction(t, "2026-09-05", "EXPENSE", "CONFIRMED", "1500", true)
	f.transaction(t, "2026-09-11", "EXPENSE", "CONFIRMED", "9999", true)
	active := f.review(t, "")
	if active.Period.Start != "2026-09-01" || active.Period.End != nil || active.Period.State != "ACTIVE" || len(active.Daily) != 10 || active.Cashflow.Expense != "1500" {
		t.Fatalf("active=%+v", active)
	}
	if active.Comparison.Mode != "ELAPSED_DAYS" || active.Comparison.EligibleCycles != 1 || *active.Comparison.Expense.Previous != "1000" || active.Comparison.Median3Available {
		t.Fatalf("elapsed comparison=%+v", active.Comparison)
	}
	for _, cycle := range active.Cycles {
		if cycle.Start == "2026-08-01" && (cycle.MeasuredUntil != "2026-08-11" || active.Comparison.Previous == nil || cycle.MeasuredUntil != active.Comparison.Previous.MeasuredUntil) {
			t.Fatalf("cycle list cutoff=%+v previous=%+v", cycle, active.Comparison.Previous)
		}
	}
	var unchangedSnapshot string
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO wealth_snapshot(household_id,observed_at,created_by_user_id) VALUES($1,'2026-08-31T08:00:00+07:00',$2) RETURNING id`, f.household, f.user).Scan(&unchangedSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO wealth_snapshot_item(snapshot_id,wealth_account_id,value_idr,source) VALUES($1,$2,40000000,'MANUAL')`, unchangedSnapshot, f.account); err != nil {
		t.Fatal(err)
	}
	unchanged := f.review(t, "?cycle_start=2026-09-01")
	unchangedFound := false
	for _, blocker := range unchanged.Quality {
		if blocker.Kind == "WEALTH_SNAPSHOT_UNCHANGED" {
			unchangedFound = true
		}
	}
	if !unchangedFound || unchanged.Wealth.Current.ID != unchangedSnapshot || unchanged.Wealth.Previous.ID != unchangedSnapshot || unchanged.Wealth.Change != nil || unchanged.Wealth.Cashflow != nil || unchanged.Wealth.Other != nil {
		t.Fatalf("unchanged snapshot blocker missing: %+v", unchanged.Quality)
	}
	f.transaction(t, "2026-09-08", "EXPENSE", "CONFIRMED", "2500", false)
	quality := f.review(t, "")
	found := false
	for _, b := range quality.Quality {
		if b.Kind == "UNCATEGORIZED_EXPENSE" {
			found = b.Count == 1 && *b.Amount == "2500"
		}
	}
	if !found {
		t.Fatalf("uncategorized blocker=%+v", quality.Quality)
	}
}

func TestCycleReviewAuthAndInvalidSelection(t *testing.T) {
	f := cycleReviewFixture(t)
	output := captureProductEvents(t)
	f.anchor(t, "2026-09-01")
	h := NewHandler(f.pool)
	for _, item := range []struct {
		query     string
		principal bool
		status    int
	}{{"", false, 403}, {"?cycle_start=invalid", true, 400}, {"?cycle_start=2026-08-01", true, 404}, {"?cycle_start=2026-10-01", true, 404}} {
		r := httptest.NewRequest("GET", "/api/v1/analytics/cycle-review"+item.query, nil)
		if item.principal {
			r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{HouseholdID: f.household, HasHousehold: true}))
		}
		w := httptest.NewRecorder()
		h.CycleReview(w, r)
		if w.Code != item.status {
			t.Fatalf("query=%s got=%d want=%d body=%s", item.query, w.Code, item.status, w.Body.String())
		}
	}
	assertProductEvents(t, output, "CYCLE_REVIEW_OPENED", 0)
}

func TestClosedCycleRecomputesAfterHistoricalCorrection(t *testing.T) {
	f := cycleReviewFixture(t)
	for _, date := range []string{"2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01"} {
		f.anchor(t, date)
	}
	previous := f.transaction(t, "2026-07-05", "EXPENSE", "CONFIRMED", "1000", true)
	current := f.transaction(t, "2026-08-05", "EXPENSE", "CONFIRMED", "1500", true)
	before := f.review(t, "?cycle_start=2026-08-01")
	if before.Cashflow.Expense != "1500" || *before.Comparison.Expense.Previous != "1000" {
		t.Fatalf("initial facts=%+v", before.Comparison)
	}
	// Synthetic correction only; canonical historical records are not deleted.
	if _, err := f.pool.Exec(context.Background(), `UPDATE transaction SET amount=amount+500 WHERE id=$1 OR id=$2`, previous, current); err != nil {
		t.Fatal(err)
	}
	after := f.review(t, "?cycle_start=2026-08-01")
	if after.Period.State != "CLOSED" || after.Cashflow.Expense != "2000" || *after.Comparison.Expense.Previous != "1500" || after.Categories[0].Amount != "2000" {
		t.Fatal("closed-cycle correction did not invalidate totals/baseline/drivers")
	}
}

func TestCycleReviewHistorySeriesWindowAndReconciliation(t *testing.T) {
	f := cycleReviewFixture(t)
	for _, date := range []string{"2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01"} {
		f.anchor(t, date)
	}
	f.transaction(t, "2026-06-05", "EXPENSE", "CONFIRMED", "1000", true)
	f.transaction(t, "2026-06-06", "EXPENSE", "CONFIRMED", "400", false)
	f.transaction(t, "2026-07-05", "EXPENSE", "CONFIRMED", "2000", true)
	f.transaction(t, "2026-07-06", "REFUND", "CONFIRMED", "500", true)
	f.transaction(t, "2026-08-05", "EXPENSE", "CONFIRMED", "1500", true)
	f.transaction(t, "2026-08-06", "TRANSFER", "CONFIRMED", "700", false)
	f.transaction(t, "2026-09-05", "EXPENSE", "CONFIRMED", "900", true)
	f.transaction(t, "2026-09-12", "EXPENSE", "CONFIRMED", "9999", true) // after the measured cutoff

	review := f.review(t, "")
	if len(review.History) != 4 {
		t.Fatalf("default window should hold every cycle (<=6): %+v", review.History)
	}
	for i, start := range []string{"2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01"} {
		if review.History[i].Start != start || review.CategoryHistory.CycleStarts[i] != start {
			t.Fatalf("history must be ascending and match the matrix columns: %+v %v", review.History, review.CategoryHistory.CycleStarts)
		}
	}
	closed, refunded, saved, active := review.History[0], review.History[1], review.History[2], review.History[3]
	if closed.State != "CLOSED" || *closed.End != "2026-07-01" || closed.MeasuredUntil != "2026-07-01" || closed.Income != "10000000" || closed.GrossExpense != "1400" || closed.Expense != "1400" || closed.Refund != "0" {
		t.Fatalf("closed=%+v", closed)
	}
	if refunded.GrossExpense != "2000" || refunded.Refund != "500" || refunded.Expense != "1500" {
		t.Fatalf("refund must reduce expense, not income: %+v", refunded)
	}
	if saved.SavingsAllocated != "700" || saved.Expense != "1500" {
		t.Fatalf("transfers are savings, never expense: %+v", saved)
	}
	if active.State != "ACTIVE" || active.End != nil || active.MeasuredUntil != "2026-09-11" || active.Expense != "900" {
		t.Fatalf("active cycle is measured to its cutoff only: %+v", active)
	}
	rows := map[string][]string{}
	for _, row := range review.CategoryHistory.Rows {
		rows[row.Name] = row.Amounts
	}
	if got := fmt.Sprint(rows["Dining"]); got != "[1000 1500 1500 900]" || fmt.Sprint(rows["Belum dikategorikan"]) != "[400 0 0 0]" {
		t.Fatalf("matrix rows=%v", rows)
	}
	for i, cycle := range review.History {
		listed := "0"
		for _, row := range review.CategoryHistory.Rows {
			listed = add(listed, row.Amounts[i])
		}
		if add(listed, review.CategoryHistory.Other.Amounts[i]) != cycle.Expense {
			t.Fatalf("column %d does not reconcile to net expense: listed=%s other=%s expense=%s", i, listed, review.CategoryHistory.Other.Amounts[i], cycle.Expense)
		}
	}

	if got := f.review(t, "?history=2").History; len(got) != 2 || got[0].Start != "2026-08-01" || got[1].Start != "2026-09-01" {
		t.Fatalf("newest window=%+v", got)
	}
	older := f.review(t, "?cycle_start=2026-07-01&history=2")
	if older.Period.Start != "2026-07-01" || len(older.History) != 2 || older.History[0].Start != "2026-06-01" || older.History[1].Start != "2026-07-01" {
		t.Fatalf("an older selection must end the window at itself: %+v", older.History)
	}
	inside := f.review(t, "?cycle_start=2026-08-01&history=2")
	if len(inside.History) != 2 || inside.History[0].Start != "2026-08-01" || inside.History[1].Start != "2026-09-01" {
		t.Fatalf("a selection inside the newest window keeps it: %+v", inside.History)
	}
	if f.review(t, "?history=1").Cashflow != review.Cashflow {
		t.Fatal("history must not change the selected cycle's facts")
	}
}
