package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/ledger"
)

// spend inserts a confirmed expense at 10:00 Jakarta time on date.
func (f reviewFixture) spend(t *testing.T, date, amount string, merchant, counterparty, user *string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,merchant_id,counterparty_name,created_by_user_id,purpose) VALUES($1,'EXPENSE','CONFIRMED',$2::numeric,($3::date::timestamp AT TIME ZONE 'Asia/Jakarta')+interval '10 hours',now(),$4,$5,$6,'GENERAL')`, f.household, amount, date, merchant, counterparty, user); err != nil {
		t.Fatal(err)
	}
}

// sectionsScenario builds four salary cycles and the active September cycle
// (measured to 2026-09-10, so ten elapsed days). Every earlier cycle has spending
// both inside and after day 10, so a wrong cut shows in the amounts.
//
//	Dining (merchant "Fixture cafe"): Jun 500|3000  Jul 700|4000  Aug 1000+2000|5000  Sep 800+1200|9999 (after cutoff)
//	uncategorized, no merchant:        Jun 0|200     Aug 100|0     (day 25 / day 2)
//
// September also holds an unresolved counterparty (700), an unattributed
// no-merchant row (300), and nine small merchants (10..90).
func sectionsScenario(t *testing.T) reviewFixture {
	t.Helper()
	f := cycleReviewFixture(t)
	for _, date := range []string{"2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01"} {
		f.anchor(t, date)
	}
	for _, row := range []struct{ date, amount string }{
		{"2026-06-04", "500"}, {"2026-06-20", "3000"},
		{"2026-07-05", "700"}, {"2026-07-12", "4000"},
		{"2026-08-05", "1000"}, {"2026-08-10", "2000"}, {"2026-08-11", "5000"},
		{"2026-09-03", "800"}, {"2026-09-09", "1200"}, {"2026-09-12", "9999"},
	} {
		f.transaction(t, row.date, "EXPENSE", "CONFIRMED", row.amount, true)
	}
	f.transaction(t, "2026-06-25", "EXPENSE", "CONFIRMED", "200", false)
	f.transaction(t, "2026-08-02", "EXPENSE", "CONFIRMED", "100", false)
	warung := "Warung Baru"
	f.spend(t, "2026-09-04", "700", nil, &warung, nil)
	f.spend(t, "2026-09-05", "300", nil, nil, nil)
	for i := 1; i <= 9; i++ {
		var id string
		if err := f.pool.QueryRow(context.Background(), `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, f.household, fmt.Sprintf("Small merchant %d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.spend(t, "2026-09-06", fmt.Sprint(i*10), &id, nil, &f.user)
	}
	return f
}

func TestCycleReviewSameDayHistoryCutsEveryCycleAtTheSameDayAndReconciles(t *testing.T) {
	f := sectionsScenario(t)
	// Another household with heavy spending on the same dates must never leak in.
	other := cycleReviewFixture(t)
	for _, date := range []string{"2026-06-01", "2026-07-01", "2026-08-01", "2026-09-01"} {
		other.anchor(t, date)
		other.transaction(t, date, "EXPENSE", "CONFIRMED", "777777", true)
	}

	review := f.review(t, "")
	h := review.CategoryHistory
	if h.SameDay == nil || h.SameDay.ElapsedDays != 10 {
		t.Fatalf("an active cycle serves a same-day matrix: %+v", h.SameDay)
	}
	if fmt.Sprint(h.SameDay.MeasuredDays) != "[10 10 10 10]" {
		t.Fatalf("every column covers the same ten days here: %v", h.SameDay.MeasuredDays)
	}
	if h.SelectedIndex == nil || *h.SelectedIndex != 3 || h.SameDay.SelectedIndex == nil || *h.SameDay.SelectedIndex != 3 {
		t.Fatalf("selected column: %v %v", h.SelectedIndex, h.SameDay.SelectedIndex)
	}
	rowOf := func(name string, same bool) ([]string, *string) {
		rows := h.Rows
		if same {
			rows = h.SameDay.Rows
		}
		for _, r := range rows {
			if r.Name == name {
				return r.Amounts, r.DeltaVsPrevious
			}
		}
		t.Fatalf("no row %q (same-day=%v)", name, same)
		return nil, nil
	}
	if strings.Join(h.SameDay.CycleStarts, ",") != "2026-06-01,2026-07-01,2026-08-01,2026-09-01" || strings.Join(h.CycleStarts, ",") != strings.Join(h.SameDay.CycleStarts, ",") {
		t.Fatalf("columns: %v / %v", h.CycleStarts, h.SameDay.CycleStarts)
	}
	// Same-day: day 1..10 of every cycle; the 09-12 spending is after the cutoff.
	dining, diningDelta := rowOf("Dining", true)
	uncategorized, uncategorizedDelta := rowOf("Belum dikategorikan", true)
	if fmt.Sprint(dining) != "[500 700 3000 2000]" || fmt.Sprint(uncategorized) != "[0 0 100 1450]" {
		t.Fatalf("same-day rows: dining=%v uncategorized=%v", dining, uncategorized)
	}
	if strings.Join(h.SameDay.Totals, ",") != "500,700,3100,3450" {
		t.Fatalf("same-day totals=%v", h.SameDay.Totals)
	}
	if *diningDelta != "-1000" || *uncategorizedDelta != "1350" || *h.SameDay.TotalDeltaVsPrevious != "350" {
		t.Fatalf("same-day deltas: %s %s %s", *diningDelta, *uncategorizedDelta, *h.SameDay.TotalDeltaVsPrevious)
	}
	// Full-cycle matrix: whole earlier cycles, the active one to its cutoff.
	fullDining, fullDiningDelta := rowOf("Dining", false)
	fullUncategorized, _ := rowOf("Belum dikategorikan", false)
	if fmt.Sprint(fullDining) != "[3500 4700 8000 2000]" || fmt.Sprint(fullUncategorized) != "[200 0 100 1450]" || *fullDiningDelta != "-6000" {
		t.Fatalf("full rows: dining=%v uncategorized=%v delta=%s", fullDining, fullUncategorized, *fullDiningDelta)
	}
	if strings.Join(h.Totals, ",") != "3700,4700,8100,3450" || *h.TotalDeltaVsPrevious != "-4650" {
		t.Fatalf("full totals=%v delta=%s", h.Totals, *h.TotalDeltaVsPrevious)
	}
	// The ranking's total delta is the same-day delta the KPI lead shows.
	if review.MerchantRanking.Total.Delta == nil || *review.MerchantRanking.Total.Delta != *h.SameDay.TotalDeltaVsPrevious {
		t.Fatalf("ranking total delta %v must equal the same-day total delta %s", review.MerchantRanking.Total.Delta, *h.SameDay.TotalDeltaVsPrevious)
	}

	// Every column reconciles to the rupiah: rows + other = totals = history expense.
	for name, m := range map[string]struct {
		rows   [][]string
		other  []string
		totals []string
	}{
		"full":    {nil, h.Other.Amounts, h.Totals},
		"sameDay": {nil, h.SameDay.Other.Amounts, h.SameDay.Totals},
	} {
		if name == "full" {
			for _, row := range h.Rows {
				m.rows = append(m.rows, row.Amounts)
			}
		} else {
			for _, row := range h.SameDay.Rows {
				m.rows = append(m.rows, row.Amounts)
			}
		}
		for i := range m.totals {
			sum := m.other[i]
			for _, row := range m.rows {
				sum = add(sum, row[i])
			}
			if sum != m.totals[i] {
				t.Fatalf("%s column %d: rows+other=%s totals=%s", name, i, sum, m.totals[i])
			}
		}
	}
	for i, cycle := range review.History {
		if cycle.Expense != h.Totals[i] {
			t.Fatalf("totals[%d]=%s must equal history expense %s", i, h.Totals[i], cycle.Expense)
		}
	}
	if fmt.Sprint(h.Totals) != fmt.Sprint(expenses(review)) || review.Cashflow.Expense != h.Totals[3] || review.Cashflow.Expense != h.SameDay.Totals[3] {
		t.Fatalf("selected column must equal cashflow.expense=%s", review.Cashflow.Expense)
	}

	// A closed cycle has no same-day view and compares full cycles only.
	closed := f.review(t, "?cycle_start=2026-08-01")
	if closed.CategoryHistory.SameDay != nil || closed.CategoryHistory.SelectedIndex == nil || *closed.CategoryHistory.SelectedIndex != 2 {
		t.Fatalf("closed cycle: sameDay=%+v selected=%v", closed.CategoryHistory.SameDay, closed.CategoryHistory.SelectedIndex)
	}
	if *closed.CategoryHistory.TotalDeltaVsPrevious != "3400" { // Aug 8100 - Jul 4700
		t.Fatalf("closed total delta=%s", *closed.CategoryHistory.TotalDeltaVsPrevious)
	}
	raw, err := json.Marshal(closed.CategoryHistory)
	if err != nil || !strings.Contains(string(raw), `"sameDay":null`) {
		t.Fatalf("closed cycle must serve sameDay:null: %s %v", raw, err)
	}
	// history=1 has no earlier column, so no deltas.
	if one := f.review(t, "?history=1").CategoryHistory; one.TotalDeltaVsPrevious != nil || one.Rows[0].DeltaVsPrevious != nil || one.SameDay == nil || one.SameDay.TotalDeltaVsPrevious != nil {
		t.Fatalf("a single column has nothing to compare: %+v", one)
	}
	// Household scoping: the other household's own totals are its own.
	if got := other.review(t, "").CategoryHistory.SameDay.Totals; strings.Join(got, ",") != "777777,777777,777777,777777" {
		t.Fatalf("other household same-day totals=%v", got)
	}
}

func expenses(review cycleReview) []string {
	out := make([]string, 0, len(review.History))
	for _, c := range review.History {
		out = append(out, c.Expense)
	}
	return out
}

func TestCycleReviewMerchantRankingReconcilesAndLinksTanpaMerchant(t *testing.T) {
	f := sectionsScenario(t)
	other := cycleReviewFixture(t) // same dates, other household
	other.anchor(t, "2026-08-01")
	other.anchor(t, "2026-09-01")
	other.transaction(t, "2026-09-03", "EXPENSE", "CONFIRMED", "555555", false)
	other.transaction(t, "2026-08-03", "EXPENSE", "CONFIRMED", "444444", false)

	review := f.review(t, "")
	r := review.MerchantRanking
	if r.Mode != "ELAPSED_DAYS" || len(r.Rows) != 10 {
		t.Fatalf("mode=%s rows=%d", r.Mode, len(r.Rows))
	}
	// Largest first, numerically: 2000, 700, 300, then 90, 80, ... 30 (nine below ten, not lexical).
	want := []string{"Fixture cafe", "Warung Baru", "Tanpa merchant", "Small merchant 9", "Small merchant 8", "Small merchant 7", "Small merchant 6", "Small merchant 5", "Small merchant 4", "Small merchant 3"}
	for i, name := range want {
		if r.Rows[i].Name != name {
			t.Fatalf("row %d = %s, want %s (rows=%+v)", i, r.Rows[i].Name, name, r.Rows)
		}
	}
	kinds := []string{r.Rows[0].Kind, r.Rows[1].Kind, r.Rows[2].Kind}
	if fmt.Sprint(kinds) != "[MERCHANT COUNTERPARTY NONE]" || r.Rows[0].ID != f.merchant || r.Rows[2].ID != "" {
		t.Fatalf("kinds=%v none-id=%q", kinds, r.Rows[2].ID)
	}
	cafe, warung, none := r.Rows[0], r.Rows[1], r.Rows[2]
	if cafe.Amount != "2000" || cafe.Count != 2 || *cafe.Previous != "3000" || *cafe.Delta != "-1000" {
		t.Fatalf("cafe row (previous cycle at the same day, not the full cycle): %+v", cafe)
	}
	if warung.Amount != "700" || *warung.Previous != "0" || *warung.Delta != "700" || none.Amount != "300" || none.Count != 1 || *none.Previous != "100" || *none.Delta != "200" {
		t.Fatalf("warung=%+v none=%+v", warung, none)
	}
	if r.Other.Amount != "30" || r.Other.Count != 2 || r.Other.MerchantCount != 2 || *r.Other.Previous != "0" || *r.Other.Delta != "30" {
		t.Fatalf("other=%+v", r.Other)
	}
	if r.Total.Amount != "3450" || r.Total.Count != 13 || *r.Total.Previous != "3100" || *r.Total.Delta != "350" {
		t.Fatalf("total=%+v", r.Total)
	}
	// Rows + other = total = net expense, now and a rupiah-exact one for the previous side.
	listed, listedPrevious, listedCount := "0", "0", 0
	for _, row := range r.Rows {
		listed, listedPrevious, listedCount = add(listed, row.Amount), add(listedPrevious, *row.Previous), listedCount+row.Count
	}
	if add(listed, r.Other.Amount) != review.Cashflow.Expense || r.Total.Amount != review.Cashflow.Expense || listedCount+r.Other.Count != r.Total.Count {
		t.Fatalf("ranking must sum to cashflow.expense=%s: listed=%s other=%s", review.Cashflow.Expense, listed, r.Other.Amount)
	}
	if add(listedPrevious, *r.Other.Previous) != *r.Total.Previous || *r.Total.Previous != *review.Comparison.Expense.Previous {
		t.Fatalf("previous side must reconcile: %s + %s vs %s / comparison %s", listedPrevious, *r.Other.Previous, *r.Total.Previous, *review.Comparison.Expense.Previous)
	}

	// A closed cycle compares against the previous FULL cycle.
	closed := f.review(t, "?cycle_start=2026-08-01").MerchantRanking
	if closed.Mode != "FULL_CYCLE" || closed.Rows[0].Name != "Fixture cafe" || closed.Rows[0].Amount != "8000" || *closed.Rows[0].Previous != "4700" || *closed.Total.Previous != "4700" || closed.Total.Amount != "8100" {
		t.Fatalf("closed ranking=%+v", closed)
	}
	oldest := f.review(t, "?cycle_start=2026-06-01").MerchantRanking
	if oldest.Total.Previous != nil || oldest.Total.Delta != nil || oldest.Rows[0].Previous != nil || oldest.Other.Previous != nil {
		t.Fatalf("the oldest cycle has nothing to compare against: %+v", oldest)
	}
	if got := other.review(t, "").MerchantRanking; got.Total.Amount != "555555" || got.Rows[0].Name != "Tanpa merchant" || got.Total.Count != 1 {
		t.Fatalf("other household ranking=%+v", got)
	}

	// The "Tanpa merchant" row links through: merchantId=none reaches exactly its transactions.
	list := func(query string) (total string, count int) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?type=SPENDING&from=2026-09-01&to=2026-09-10&"+query, nil)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: f.user, Memberships: []auth.Membership{{HouseholdID: f.household, Role: "OWNER"}}}))
		response := httptest.NewRecorder()
		ledger.NewHandler(f.pool).ListTransactions(response, request)
		if response.Code != 200 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var rows []struct {
			Amount string `json:"amount"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		total = "0"
		for _, row := range rows {
			total = add(total, row.Amount)
		}
		return total, len(rows)
	}
	if total, count := list("merchantId=none"); total != none.Amount || count != none.Count {
		t.Fatalf("merchantId=none total=%s count=%d, ranking row %+v", total, count, none)
	}
	if total, count := list("merchantId=" + cafe.ID); total != cafe.Amount || count != cafe.Count {
		t.Fatalf("merchant drill-down total=%s count=%d, ranking row %+v", total, count, cafe)
	}
}

func TestCycleReviewMemberAttributionServesKindShareAndFixedOrder(t *testing.T) {
	f := sectionsScenario(t)
	review := f.review(t, "")
	members := review.Members
	if len(members) != 2 || members[0].Kind != "AUTOMATIC" || members[0].ID != "" || members[1].Kind != "MEMBER" || members[1].ID != f.user {
		t.Fatalf("the automatic bucket comes first, then members by name: %+v", members)
	}
	if members[0].Amount != "1000" || members[0].Count != 2 || members[1].Amount != "2450" {
		t.Fatalf("amounts=%+v", members)
	}
	// 1000/3450 = 28.9855% and 2450/3450 = 71.0145%: largest remainder keeps the sum at 100%.
	if *members[0].Share != "0.2899" || *members[1].Share != "0.7101" {
		t.Fatalf("shares=%s %s", *members[0].Share, *members[1].Share)
	}
	if add(members[0].Amount, members[1].Amount) != review.Cashflow.Expense {
		t.Fatalf("members must sum to net expense %s", review.Cashflow.Expense)
	}
}
