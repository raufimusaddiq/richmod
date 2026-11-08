package reviewdomain

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The database owns the transaction status machine (migration 00097): a voided
// transaction cannot be confirmed again, nothing returns to PENDING, and a salary event follows its transaction's status.
func TestTransactionStatusMachineAndSalaryEventSync(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
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
	f := seedMergedScreenshot(t, ctx, pool, "status-machine")
	// Nothing moves back to PENDING.
	if _, err := pool.Exec(ctx, `UPDATE transaction SET status='PENDING',confirmed_at=NULL WHERE id=$1`, f.original); err == nil {
		t.Fatal("a confirmed transaction went back to pending")
	}
	var salarySource, salaryEvent string
	stamp := fmt.Sprintf("%d", time.Now().UnixNano())
	must(pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,employer,normalized_employer,is_primary) VALUES($1,'Acme '||$2,'acme '||$2,true) RETURNING id::text`, f.household, stamp).Scan(&salarySource))
	must(pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,'2026-10-01','2026-10-25',7500,$3,'CONFIRMED',$4) RETURNING id::text`, salarySource, f.household, f.original, f.source).Scan(&salaryEvent))

	_, err = pool.Exec(ctx, `UPDATE transaction SET status='VOIDED',confirmed_at=NULL,voided_at=now() WHERE id=$1`, f.original)
	must(err)
	var status string
	must(pool.QueryRow(ctx, `SELECT status FROM salary_event WHERE id=$1`, salaryEvent).Scan(&status))
	if status != "VOIDED" {
		t.Fatalf("salary event status = %s after its transaction was voided", status)
	}
	// A voided transaction is never confirmed again.
	if _, err := pool.Exec(ctx, `UPDATE transaction SET status='CONFIRMED',confirmed_at=now(),voided_at=NULL WHERE id=$1`, f.original); err == nil {
		t.Fatal("a voided transaction was confirmed again")
	}
	// A reversed merge may reopen it, and confirming it then revives the event.
	_, err = pool.Exec(ctx, `UPDATE transaction SET status='NEEDS_REVIEW',voided_at=NULL WHERE id=$1`, f.original)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE transaction SET status='CONFIRMED',confirmed_at=now() WHERE id=$1`, f.original)
	must(err)
	must(pool.QueryRow(ctx, `SELECT status FROM salary_event WHERE id=$1`, salaryEvent).Scan(&status))
	if status != "CONFIRMED" {
		t.Fatalf("salary event status = %s after its transaction was confirmed again", status)
	}
}
