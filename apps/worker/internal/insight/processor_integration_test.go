package insight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestCommentaryPersistenceCompatibilityAndFailureIsolation(t *testing.T) {
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
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var household, user, source, salary, category string
	stamp := time.Now().UnixNano()
	check(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("analyst %d", stamp)).Scan(&household))
	check(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("analyst-%d@example.test", stamp)).Scan(&user))
	check(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'SYSTEM',$2,'2026-08-01T00:00:00+07:00',decode(md5($2),'hex'),'PROCESSED') RETURNING id`, household, fmt.Sprint(stamp)).Scan(&source))
	check(pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Fixture salary','fixture salary',true) RETURNING id`, household, user).Scan(&salary))
	check(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Dining','dining') RETURNING id`, household).Scan(&category))
	for _, date := range []string{"2026-08-01", "2026-09-01"} {
		var income string
		check(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'INCOME','CONFIRMED',10000000,($2::date::timestamp AT TIME ZONE 'Asia/Jakarta'),now()) RETURNING id`, household, date).Scan(&income))
		_, err = pool.Exec(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,$3::date,$3::date,10000000,$4,'CONFIRMED',$5)`, salary, household, date, income, source)
		check(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at,category_id) VALUES($1,'EXPENSE','CONFIRMED',1400000,'2026-08-05T12:00:00+07:00',now(),$2)`, household, category)
	check(err)
	var historical string
	check(pool.QueryRow(ctx, `INSERT INTO insight(household_id,period,status,input_metrics_json,prompt_version,data_completeness,requested_by_user_id,generated_text,completed_at) VALUES($1,'2026-07-01','SUCCEEDED','{}','finance-insight-v2',1,$2,'Historical text',now()) RETURNING id`, household, user).Scan(&historical))
	for _, tc := range []struct {
		name, version, completeness string
		fail                        bool
		wantStatus                  string
		phases                      int
	}{
		{"success", promptVersion, "1", false, "SUCCEEDED", 2},
		{"low coverage", promptVersion, "0.60", false, "FAILED", 0},
		{"legacy pending", "finance-insight-v2", "1", false, "FAILED", 0},
		{"gateway failure", promptVersion, "1", true, "FAILED", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var id string
			check(pool.QueryRow(ctx, `INSERT INTO insight(household_id,period,status,input_metrics_json,prompt_version,data_completeness,requested_by_user_id) VALUES($1,'2026-08-01','PENDING','{"period_kind":"SALARY_CYCLE","period_start":"2026-08-01"}',$2,$3,$4) RETURNING id`, household, tc.version, tc.completeness, user).Scan(&id))
			g := &analystGateway{responses: []gateway.AgentResponse{overviewPhase(), renderPhase("Tidak ada perubahan berarti untuk dibahas.")}}
			if tc.fail {
				g.err = fmt.Errorf("gateway unavailable private-detail")
			}
			p := NewProcessor(pool, g)
			if tc.fail {
				if err := p.Process(ctx, id, false); err == nil {
					t.Fatal("failed generation must reach queue retry policy")
				}
				var retryStatus string
				check(pool.QueryRow(ctx, `SELECT status FROM insight WHERE id=$1`, id).Scan(&retryStatus))
				if retryStatus != "PENDING" {
					t.Fatalf("retry status=%s", retryStatus)
				}
				if err := p.Process(ctx, id, true); err == nil {
					t.Fatal("exhausted generation must fail the queue job")
				}
				var reason string
				check(pool.QueryRow(ctx, `SELECT after_json->>'reason' FROM audit_log WHERE entity_id=$1 AND action='FAIL_INSIGHT'`, id).Scan(&reason))
				if reason != "generate insight: gateway_failure" {
					t.Fatalf("failure reason=%s", reason)
				}
			} else {
				check(p.Process(ctx, id, false))
			}
			check(p.Process(ctx, id, true))
			var status string
			var text, confidence *string
			var metrics json.RawMessage
			check(pool.QueryRow(ctx, `SELECT status,generated_text,confidence::text,input_metrics_json FROM insight WHERE id=$1`, id).Scan(&status, &text, &confidence, &metrics))
			wantPhases := tc.phases
			if tc.fail {
				wantPhases++
			}
			if status != tc.wantStatus || len(g.requests) != wantPhases || confidence != nil {
				t.Fatalf("status=%s phases=%d confidence=%v", status, len(g.requests), confidence)
			}
			if tc.wantStatus == "FAILED" && text != nil {
				t.Fatal("failure manufactured analytical prose")
			}
			if tc.wantStatus == "SUCCEEDED" {
				var stored map[string]any
				check(json.Unmarshal(metrics, &stored))
				if text == nil || *text != "Tidak ada perubahan berarti untuk dibahas." || stored["tool_contract"] != promptVersion || len(stored["tool_reads"].([]any)) != 2 {
					t.Fatalf("commentary/audit=%s %v", metrics, text)
				}
			}
			var audits int
			check(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_id=$2 AND action IN ('COMPLETE_INSIGHT','FAIL_INSIGHT')`, household, id).Scan(&audits))
			if audits != 1 {
				t.Fatalf("duplicate audit=%d", audits)
			}
		})
	}
	t.Run("timeout retry recovers without terminal failure audit", func(t *testing.T) {
		var id string
		check(pool.QueryRow(ctx, `INSERT INTO insight(household_id,period,status,input_metrics_json,prompt_version,data_completeness,requested_by_user_id) VALUES($1,'2026-08-01','PENDING','{"period_start":"2026-08-01"}',$2,1,$3) RETURNING id`, household, promptVersion, user).Scan(&id))
		g := &analystGateway{err: context.DeadlineExceeded, responses: []gateway.AgentResponse{overviewPhase(), renderPhase("Data tersedia.")}}
		p := NewProcessor(pool, g)
		if err := p.Process(ctx, id, false); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("retryable timeout=%v", err)
		}
		g.err = nil
		g.requests = nil
		check(p.Process(ctx, id, false))
		check(p.Process(ctx, id, true))
		var status string
		var failedAudits, completeAudits int
		check(pool.QueryRow(ctx, `SELECT status FROM insight WHERE id=$1`, id).Scan(&status))
		check(pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action='FAIL_INSIGHT'),count(*) FILTER (WHERE action='COMPLETE_INSIGHT') FROM audit_log WHERE entity_id=$1`, id).Scan(&failedAudits, &completeAudits))
		if status != "SUCCEEDED" || failedAudits != 0 || completeAudits != 1 {
			t.Fatalf("status=%s failed=%d completed=%d", status, failedAudits, completeAudits)
		}
	})
	var oldText string
	check(pool.QueryRow(ctx, `SELECT generated_text FROM insight WHERE id=$1`, historical).Scan(&oldText))
	if oldText != "Historical text" {
		t.Fatal("historical insight changed")
	}
	var transactions int
	check(pool.QueryRow(ctx, `SELECT count(*) FROM transaction WHERE household_id=$1`, household).Scan(&transactions))
	if transactions != 3 {
		t.Fatal("commentary changed financial state")
	}
	var _ Gateway = (*gateway.Client)(nil)
}
