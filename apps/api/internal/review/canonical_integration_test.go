package review

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestResolveResidualAllocationValidationAndStaleBasis(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	household, user, _ := seedTransferReviewOwner(t, pool, stamp)
	var source, salarySource, start, end, review, account, foreign string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'{}','PROCESSED') RETURNING id`, household, fmt.Sprint(stamp)).Scan(&source))
	must(pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id`, household, user).Scan(&salarySource))
	for _, row := range []struct {
		date, period string
		out          *string
	}{{"2026-01-01", "2026-01-01", &start}, {"2026-02-01", "2026-02-01", &end}} {
		var tx string
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id`, household, row.date).Scan(&tx))
		must(pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$4::date,1000000,$5,'CONFIRMED',$6) RETURNING id`, salarySource, household, row.period, row.date, tx, source).Scan(row.out))
	}
	var caseID string
	must(pool.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-01-01','2026-02-01',1000000,0,0,1000000) RETURNING id`, household, start, end).Scan(&caseID))
	must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','OPEN') RETURNING id`, household, caseID).Scan(&review))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Savings','ASSET','BANK','SAVINGS') RETURNING id`, household).Scan(&account))
	var otherHousehold string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Other %d", stamp)).Scan(&otherHousehold))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Other','ASSET','BANK','SAVINGS') RETURNING id`, otherHousehold).Scan(&foreign))
	resolve := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+review+"/resolve", bytes.NewBufferString(body))
		r.SetPathValue("id", review)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}))
		w := httptest.NewRecorder()
		NewHandler(pool).Resolve(w, r)
		return w
	}
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + account + `","amountIdr":"999999"}]}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("mismatch=%d %s", w.Code, w.Body.String())
	}
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + foreign + `","amountIdr":"1000000"}]}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("foreign=%d %s", w.Code, w.Body.String())
	}
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + account + `","amountIdr":"500000"},{"wealthAccountId":"` + account + `","amountIdr":"500000"}]}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("duplicate=%d %s", w.Code, w.Body.String())
	}
	_, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',500000,'2026-01-15','GENERAL',now())`, household)
	must(err)
	if w := resolve(`{"action":"ALLOCATE_RETAINED_BALANCE","values":{"allocations":[{"wealthAccountId":"` + account + `","amountIdr":"1000000"}]}}`); w.Code != http.StatusConflict {
		t.Fatalf("stale=%d %s", w.Code, w.Body.String())
	}
	var residual string
	var allocations int
	must(pool.QueryRow(ctx, `SELECT basis_residual_idr::text,(SELECT count(*) FROM cycle_residual_allocation WHERE cycle_residual_case_id=$1) FROM cycle_residual_case WHERE id=$1`, caseID).Scan(&residual, &allocations))
	if residual != "500000" || allocations != 0 {
		t.Fatalf("residual=%s allocations=%d", residual, allocations)
	}
}

func TestResolveUnknownBankTemplateIgnore(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	stamp := time.Now().UnixNano()
	householdID, userID, _ := seedTransferReviewOwner(t, pool, stamp)
	var sourceID, reviewID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("unknown-bank-%d", stamp), []byte(fmt.Sprintf("unknown-bank-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN') RETURNING id`, householdID, sourceID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/resolve", bytes.NewBufferString(`{"action":"IGNORE","values":{}}`))
	request.SetPathValue("id", reviewID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	NewHandler(pool).Resolve(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var sourceStatus, reviewStatus, resolution string
	if err := pool.QueryRow(ctx, `SELECT s.processing_status,ri.status,ri.resolution_action FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id WHERE ri.id=$1`, reviewID).Scan(&sourceStatus, &reviewStatus, &resolution); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "IGNORED" || reviewStatus != "RESOLVED" || resolution != "IGNORE" {
		t.Fatalf("source=%s review=%s resolution=%s", sourceStatus, reviewStatus, resolution)
	}
}
