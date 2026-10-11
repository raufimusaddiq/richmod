package analyticscore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSessionExcludesOnlyCurrentSourceFromNativeQuality(t *testing.T) {
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
	household := insert(`INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Native quality %d", stamp))
	otherHousehold := insert(`INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Other native quality %d", stamp))
	user := insert(`INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Review member','unused') RETURNING id`, fmt.Sprintf("native-quality-%d@example.test", stamp))
	salary := insert(`INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Fixture salary','fixture salary',true) RETURNING id`, household, user)
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	sourceIDs := []string{}
	source := func(owner, external, date, status string) string {
		t.Helper()
		id := insert(`INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,($3::date::timestamp AT TIME ZONE 'Asia/Jakarta'),decode(md5($2::text),'hex'),$4) RETURNING id`, owner, external, date, status)
		sourceIDs = append(sourceIDs, id)
		return id
	}
	for _, date := range []string{"2026-08-01", "2026-09-01"} {
		event := source(household, "salary-"+date, date, "PROCESSED")
		transaction := insert(`INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,created_by_user_id) VALUES($1,'INCOME','CONFIRMED',10000000,($2::date::timestamp AT TIME ZONE 'Asia/Jakarta'),now(),$3) RETURNING id`, household, date, user)
		if _, err := pool.Exec(ctx, `INSERT INTO salary_event(household_id,salary_source_id,payroll_period,pay_date,net_pay,transaction_id,source_event_id,status) VALUES($1,$2,$3::date,$3::date,10000000,$4,$5,'CONFIRMED')`, household, salary, date, transaction, event); err != nil {
			t.Fatal(err)
		}
	}
	self := source(household, "current-analytics-turn", "2026-09-10", "RECEIVED")
	for _, status := range []string{"RECEIVED", "PROCESSING", "FAILED"} {
		source(household, "other-"+status, "2026-09-09", status)
	}
	source(household, "previous-failure", "2026-08-20", "FAILED")
	source(otherHousehold, "unrelated-failure", "2026-09-09", "FAILED")
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	session := NewSession(pool, household, now, self)
	processingCount := func(blockers []reviewBlocker) int {
		for _, blocker := range blockers {
			if blocker.Kind == "PROCESSING_INCOMPLETE" {
				return blocker.Count
			}
		}
		return 0
	}
	for _, tc := range []struct {
		name, selected, args  string
		wantSession, wantLoad int
	}{
		{"current", "", `{"cycle_start":null}`, 3, 4},
		{"previous", "2026-08-01", `{"cycle_start":"2026-08-01"}`, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := session.Read(ctx, "get_cycle_data_quality", json.RawMessage(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			period := payload["period"].(reviewPeriod)
			if period.Kind != "SALARY_CYCLE" || (tc.name == "current" && period.State != "ACTIVE") || (tc.name == "previous" && period.State != "CLOSED") {
				t.Fatalf("period=%+v", period)
			}
			if got := processingCount(payload["blockers"].([]reviewBlocker)); got != tc.wantSession {
				t.Fatalf("native unfinished count=%d want=%d", got, tc.wantSession)
			}
			if payload["data_completeness_scope"] != "CASHFLOW" || payload["data_completeness"] != "1.0000" {
				t.Fatalf("cashflow coverage metadata=%v", payload)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range sourceIDs {
				if strings.Contains(string(encoded), id) {
					t.Fatal("native payload leaked source ID")
				}
			}
			facts, err := Load(ctx, pool, household, tc.selected, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := processingCount(facts.Quality); got != tc.wantLoad {
				t.Fatalf("ordinary Load unfinished count=%d want=%d", got, tc.wantLoad)
			}
		})
	}
	if _, err := session.Read(ctx, "get_cycle_data_quality", json.RawMessage(fmt.Sprintf(`{"cycle_start":null,"current_source_event_id":%q}`, self))); err == nil {
		t.Fatal("model must not control source exclusion")
	}
}
