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
	var facts cycleReview
	if err := json.Unmarshal(w.Body.Bytes(), &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestCycleReviewRefundBaselinesDriversWealthAndIsolation(t *testing.T) {
	f := cycleReviewFixture(t)
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
}
