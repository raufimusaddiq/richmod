package residual

// UIRC-01A regression: once the missing transaction is recorded through the
// ordinary Telegram intake, the open cycle review must be recomputed in the same
// canonical transaction, so the residual refresh cannot be skipped by a failed
// follow-up job. The review stays open while the positive residual remains and
// closes when the new expense removes it.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func TestConfirmedIntakeRefreshesOpenCycleResidual(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	var household, user, source, primary string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Refresh %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("refresh-%d@example.test", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'{}','PROCESSED') RETURNING id`, household, fmt.Sprintf("refresh-%d", stamp)).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Primary','primary',true) RETURNING id`, household, user).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	insertSalary := func(date, period string) string {
		var tx, event string
		if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,created_by_user_id,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',16000000,$2::date,$3,'GENERAL',now()) RETURNING id`, household, date, user).Scan(&tx); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$4::date,16000000,$5,'CONFIRMED',$6) RETURNING id`, primary, household, period, date, tx, source).Scan(&event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	insertSalary("2026-08-25", "2026-08-01")
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,created_by_user_id,purpose,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',9000000,'2026-08-26',$2,'GENERAL',now())`, household, user); err != nil {
		t.Fatal(err)
	}
	end := insertSalary("2026-09-25", "2026-09-01")
	p := New(pool)
	if err = p.Generate(ctx, Payload{HouseholdID: household, EndSalaryEventID: end}); err != nil {
		t.Fatal(err)
	}
	var itemID, caseID, basis string
	if err = pool.QueryRow(ctx, `SELECT ri.id::text,c.id::text,c.basis_residual_idr::text FROM review_item ri JOIN cycle_residual_case c ON c.id=ri.cycle_residual_case_id WHERE ri.household_id=$1 AND ri.review_type='CYCLE_RESIDUAL_ALLOCATION'`, household).Scan(&itemID, &caseID, &basis); err != nil {
		t.Fatal(err)
	}
	if basis != "7000000" {
		t.Fatalf("seed residual = %s, want 7000000", basis)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = reviewdomain.RefreshOpenCycleResiduals(ctx, tx, household, time.Date(2026, 9, 20, 12, 0, 0, 0, time.FixedZone("WIB", 7*3600)), user); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var refreshed, status string
	if err = pool.QueryRow(ctx, `SELECT basis_residual_idr::text FROM cycle_residual_case WHERE id=$1`, caseID).Scan(&refreshed); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if refreshed != "7000000" || status != "PENDING_SEND" && status != "OPEN" {
		t.Fatalf("unrelated confirmed transaction changed the review: residual=%s status=%s", refreshed, status)
	}
	// The missing transaction lands inside the cycle: the residual drops to 0 and
	// the open review closes as no-longer-applicable instead of staying stale.
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,created_by_user_id,purpose,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',7000000,'2026-09-20',$2,'GENERAL',now())`, household, user); err != nil {
		t.Fatal(err)
	}
	if err = reviewdomain.RefreshOpenCycleResiduals(ctx, tx, household, time.Date(2026, 9, 20, 12, 0, 0, 0, time.FixedZone("WIB", 7*3600)), user); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT basis_residual_idr::text FROM cycle_residual_case WHERE id=$1`, caseID).Scan(&refreshed); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM review_item WHERE id=$1`, itemID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if refreshed != "0" || status != "RESOLVED" {
		t.Fatalf("missing transaction did not refresh the review: residual=%s status=%s", refreshed, status)
	}
}
