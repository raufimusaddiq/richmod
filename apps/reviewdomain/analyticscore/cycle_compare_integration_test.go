package analyticscore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The active cycle is longer than the previous one and the previous cycle's
// first-day spending happens just after Jakarta midnight on the anchor day.
func TestActiveCycleLongerThanPreviousComparesAgainstWholePreviousCycle(t *testing.T) {
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
	insert := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	stamp := time.Now().UnixNano()
	household := insert(`INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Cycle compare %d", stamp))
	user := insert(`INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Member','unused') RETURNING id`, fmt.Sprintf("cycle-compare-%d@example.test", stamp))
	salary := insert(`INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Fixture salary','fixture salary',true) RETURNING id`, household, user)
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"2026-08-26", "2026-09-25", "2026-10-23"} {
		event := insert(`INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,($3::date::timestamp AT TIME ZONE 'Asia/Jakarta'),decode(md5($2::text),'hex'),'PROCESSED') RETURNING id`, household, "salary-"+date, date)
		tx := insert(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,created_by_user_id) VALUES($1,'INCOME','CONFIRMED',10000000,($2::date::timestamp AT TIME ZONE 'Asia/Jakarta'),now(),$3) RETURNING id`, household, date, user)
		if _, err := pool.Exec(ctx, `INSERT INTO salary_event(household_id,salary_source_id,payroll_period,pay_date,net_pay,transaction_id,source_event_id,status) VALUES($1,$2,$3::date,$3::date,10000000,$4,$5,'CONFIRMED')`, household, salary, date, tx, event); err != nil {
			t.Fatal(err)
		}
	}
	expense := func(at string, amount int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,created_by_user_id) VALUES($1,'EXPENSE','CONFIRMED',$2,$3::timestamp AT TIME ZONE 'Asia/Jakarta',now(),$4)`, household, amount, at, user); err != nil {
			t.Fatal(err)
		}
	}
	expense("2026-08-30 12:00", 1000000) // older cycle
	expense("2026-09-25 00:30", 2760000) // first Jakarta day of the previous cycle, after midnight but before any anchor time-of-day
	expense("2026-10-10 12:00", 8199839) // later in the previous cycle
	expense("2026-10-22 23:59", 0+1)     // last Jakarta day of the previous cycle
	expense("2026-10-23 08:00", 500000)  // first day of the active cycle
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.FixedZone("WIB", 7*3600)) // active day 30, previous is 28 days
	facts, err := LoadWithHistory(ctx, pool, household, "", now, 3)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Comparison.Previous == nil || facts.Comparison.Previous.Start != "2026-09-25" || facts.Comparison.Previous.MeasuredUntil != "2026-10-23" {
		t.Fatalf("previous cycle must be the immediate 28-day cycle measured whole: %+v", facts.Comparison.Previous)
	}
	if got := facts.Comparison.Expense.Previous; got == nil || *got != "10959840" {
		t.Fatalf("previous expense=%v, want the whole previous cycle 10959840", got)
	}
	if facts.Comparison.Expense.PreviousFull == nil || *facts.Comparison.Expense.PreviousFull != "10959840" {
		t.Fatalf("previous full expense=%v", facts.Comparison.Expense.PreviousFull)
	}
}
