package residual

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGenerateCreatesOneCaseAndReviewForPrimaryCycleOnly(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	stamp := time.Now().UnixNano()
	var household, user, source, primary, ordinary string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Residual %d", stamp)).Scan(&household); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("residual-%d@example.test", stamp)).Scan(&user); err != nil { t.Fatal(err) }
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),'{}','PROCESSED') RETURNING id`, household, fmt.Sprintf("residual-%d", stamp)).Scan(&source); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Primary','primary',true) RETURNING id`, household, user).Scan(&primary); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Ordinary','ordinary',false) RETURNING id`, household, user).Scan(&ordinary); err != nil { t.Fatal(err) }
	insertSalary := func(sourceID, date, period string) string {
		var tx, event string
		if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,created_by_user_id,purpose,confirmed_at) VALUES($1,'INCOME','CONFIRMED',16000000,$2::date,$3,'GENERAL',now()) RETURNING id`, household, date, user).Scan(&tx); err != nil { t.Fatal(err) }
		if err := pool.QueryRow(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$4::date,16000000,$5,'CONFIRMED',$6) RETURNING id`, sourceID, household, period, date, tx, source).Scan(&event); err != nil { t.Fatal(err) }
		return event
	}
	start := insertSalary(primary, "2026-08-25", "2026-08-01")
	_ = start
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,created_by_user_id,purpose,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',9000000,'2026-08-26',$2,'GENERAL',now()),($1,'TRANSFER','CONFIRMED',6000000,'2026-08-27',$2,'SAVINGS_TRANSFER',now())`, household, user); err != nil { t.Fatal(err) }
	ordinaryEnd := insertSalary(ordinary, "2026-09-24", "2026-09-01")
	p := New(pool)
	if err = p.Generate(ctx, Payload{HouseholdID: household, EndSalaryEventID: ordinaryEnd}); err == nil { t.Fatal("ordinary salary must not close a cycle") }
	end := insertSalary(primary, "2026-09-25", "2026-09-01")
	for range 2 { if err = p.Generate(ctx, Payload{HouseholdID: household, EndSalaryEventID: end}); err != nil { t.Fatal(err) } }
	var cases, reviews int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM cycle_residual_case WHERE household_id=$1`, household).Scan(&cases); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1 AND review_type='CYCLE_RESIDUAL_ALLOCATION'`, household).Scan(&reviews); err != nil { t.Fatal(err) }
	if cases != 1 || reviews != 1 { t.Fatalf("cases=%d reviews=%d", cases, reviews) }
}
