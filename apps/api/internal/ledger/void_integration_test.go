package ledger

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// The web "Batalkan transaksi" action: a CONFIRMED WEB_MANUAL transaction is voided in
// place (nothing deleted), audited, and its evidence and salary link follow.
func TestVoidWebManualTransaction(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var household, foreign, user string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Void own') RETURNING id`).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Void foreign') RETURNING id`).Scan(&foreign))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Voider','unused') RETURNING id`, fmt.Sprintf("void-%d@example.test", time.Now().UnixNano())).Scan(&user))
	h := NewHandler(pool)
	principal := auth.Principal{UserID: user, HouseholdID: household, HasHousehold: true, Memberships: []auth.Membership{{HouseholdID: household, Role: "MEMBER"}}}
	as := func(r *http.Request, p auth.Principal) *http.Request {
		return r.WithContext(auth.ContextWithPrincipal(r.Context(), p))
	}
	create := func() string {
		t.Helper()
		body := `{"type":"EXPENSE","amount":"25000","description":"Salah catat"}`
		w := httptest.NewRecorder()
		h.CreateManualTransaction(w, as(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body)), principal))
		if w.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", w.Code, w.Body)
		}
		var id string
		must(pool.QueryRow(ctx, `SELECT id FROM transaction WHERE household_id=$1 AND description='Salah catat' ORDER BY created_at DESC LIMIT 1`, household).Scan(&id))
		return id
	}
	void := func(id string, p auth.Principal) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/"+id+"/void", nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.VoidTransaction(w, as(r, p))
		return w
	}

	t.Run("confirmed_web_manual_is_voided_audited_and_kept", func(t *testing.T) {
		id := create()
		var source string
		must(pool.QueryRow(ctx, `SELECT s.source_type FROM transaction_evidence e JOIN source_event s ON s.id=e.source_event_id WHERE e.transaction_id=$1`, id).Scan(&source))
		if source != "WEB_MANUAL" {
			t.Fatalf("fixture source=%s", source)
		}
		if w := void(id, principal); w.Code != http.StatusNoContent {
			t.Fatalf("void status=%d body=%s", w.Code, w.Body)
		}
		var status string
		var voidedAt, confirmedAt *time.Time
		must(pool.QueryRow(ctx, `SELECT status,voided_at,confirmed_at FROM transaction WHERE id=$1`, id).Scan(&status, &voidedAt, &confirmedAt))
		if status != "VOIDED" || voidedAt == nil || confirmedAt != nil {
			t.Fatalf("status=%s voided_at=%v confirmed_at=%v", status, voidedAt, confirmedAt)
		}
		var action, actor, before, after string
		var count int
		must(pool.QueryRow(ctx, `SELECT count(*),max(action),max(actor_id::text),max(before_json->>'status'),max(after_json->>'status') FROM audit_log WHERE household_id=$1 AND entity_type='transaction' AND entity_id=$2 AND action='VOIDED'`, household, id).Scan(&count, &action, &actor, &before, &after))
		if count != 1 || actor != user || before != "CONFIRMED" || after != "VOIDED" {
			t.Fatalf("audit count=%d actor=%s %s->%s", count, actor, before, after)
		}
		var evidence int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM transaction_evidence WHERE transaction_id=$1`, id).Scan(&evidence))
		if evidence != 1 {
			t.Fatalf("void must keep evidence, got %d rows", evidence)
		}
		// The list still returns the row (history), now as VOIDED.
		r := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+id, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.GetTransaction(w, as(r, principal))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"VOIDED"`) {
			t.Fatalf("detail after void: %d %s", w.Code, w.Body)
		}
		// Voiding twice is refused and writes no second audit row.
		if w := void(id, principal); w.Code != http.StatusConflict {
			t.Fatalf("second void status=%d", w.Code)
		}
		must(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='VOIDED'`, id).Scan(&count))
		if count != 1 {
			t.Fatalf("audit rows after second void=%d", count)
		}
	})

	t.Run("another_household_gets_404_and_nothing_changes", func(t *testing.T) {
		id := create()
		other := auth.Principal{UserID: user, HouseholdID: foreign, HasHousehold: true, Memberships: []auth.Membership{{HouseholdID: foreign, Role: "OWNER"}}}
		if w := void(id, other); w.Code != http.StatusNotFound {
			t.Fatalf("status=%d", w.Code)
		}
		var status string
		must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, id).Scan(&status))
		if status != "CONFIRMED" {
			t.Fatalf("status=%s", status)
		}
	})

	t.Run("void_updates_salary_event_and_open_cycle_review", func(t *testing.T) {
		var hh, usr, source, salarySource, startEvent, endEvent, startTx, expenseTx, caseID, review string
		must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Void cycle') RETURNING id`).Scan(&hh))
		must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Cycle voider','unused') RETURNING id`, fmt.Sprintf("void-cycle-%d@example.test", time.Now().UnixNano())).Scan(&usr))
		p := auth.Principal{UserID: usr, HouseholdID: hh, HasHousehold: true, Memberships: []auth.Membership{{HouseholdID: hh, Role: "OWNER"}}}
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'{}','PROCESSED') RETURNING id`, hh, fmt.Sprint(time.Now().UnixNano())).Scan(&source))
		must(pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test','test',true) RETURNING id`, hh, usr).Scan(&salarySource))
		for _, row := range []struct {
			date string
			out  *string
			tx   *string
		}{{"2026-01-01", &startEvent, &startTx}, {"2026-02-01", &endEvent, new(string)}} {
			must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,$2::date,'GENERAL',now()) RETURNING id`, hh, row.date).Scan(row.tx))
			must(pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,1000000,$4,'CONFIRMED',$5) RETURNING id`, salarySource, hh, row.date, *row.tx, source).Scan(row.out))
		}
		must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',300000,'2026-01-10',now()) RETURNING id`, hh).Scan(&expenseTx))
		must(pool.QueryRow(ctx, `INSERT INTO cycle_residual_case(household_id,start_salary_event_id,end_salary_event_id,cycle_start,cycle_end,basis_income_idr,basis_expense_idr,basis_savings_idr,basis_residual_idr) VALUES($1,$2,$3,'2026-01-01','2026-02-01',1000000,300000,0,700000) RETURNING id`, hh, startEvent, endEvent).Scan(&caseID))
		must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,cycle_residual_case_id,review_type,status,decision) VALUES($1,$2,'CYCLE_RESIDUAL_ALLOCATION','OPEN','{"version":1,"reasonCode":"CYCLE_RESIDUAL_ALLOCATION","knownFacts":{"residual_idr":"700000"},"allowedActions":["ALLOCATE_RETAINED_BALANCE","TRANSACTION_MISSING","LEAVE_UNALLOCATED"]}'::jsonb) RETURNING id`, hh, caseID).Scan(&review))

		if w := void(expenseTx, p); w.Code != http.StatusNoContent {
			t.Fatalf("void expense=%d %s", w.Code, w.Body)
		}
		var expense, residual, shown, state string
		must(pool.QueryRow(ctx, `SELECT c.basis_expense_idr::text,c.basis_residual_idr::text,ri.decision->'knownFacts'->>'residual_idr',ri.status FROM cycle_residual_case c JOIN review_item ri ON ri.id=$2 WHERE c.id=$1`, caseID, review).Scan(&expense, &residual, &shown, &state))
		if expense != "0" || residual != "1000000" || shown != "1000000" || state != "OPEN" {
			t.Fatalf("open cycle review not refreshed: expense=%s residual=%s shown=%s state=%s", expense, residual, shown, state)
		}

		if w := void(startTx, p); w.Code != http.StatusNoContent {
			t.Fatalf("void salary=%d %s", w.Code, w.Body)
		}
		var eventStatus string
		must(pool.QueryRow(ctx, `SELECT status FROM salary_event WHERE id=$1`, startEvent).Scan(&eventStatus))
		if eventStatus != "VOIDED" {
			t.Fatalf("salary_event status=%s", eventStatus)
		}
		must(pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, review).Scan(&state))
		if state != "RESOLVED" {
			t.Fatalf("a cycle with no income left must close its review, status=%s", state)
		}
	})

	t.Run("open_review_is_409_and_untouched", func(t *testing.T) {
		id := create()
		must(func() error {
			_, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'TRANSACTION_FACTS_MISSING','OPEN','{"reasonCode":"TRANSACTION_FACTS_MISSING","missingFacts":["category"],"allowedActions":["CONFIRM_REVIEW","IGNORE"]}')`, household, id)
			return err
		}())
		w := void(id, principal)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "review endpoint") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		var status string
		var audits int
		must(pool.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM audit_log WHERE entity_id=t.id AND action='VOIDED') FROM transaction t WHERE id=$1`, id).Scan(&status, &audits))
		if status != "CONFIRMED" || audits != 0 {
			t.Fatalf("status=%s audits=%d", status, audits)
		}
	})
}
