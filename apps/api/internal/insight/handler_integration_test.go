package insight

import (
	"context"
	"encoding/json"
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

func TestGenerateCycleInsightUsesTrueSalaryAnchor(t *testing.T) {
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
	var householdID, userID, sourceID, transactionID, salarySourceID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Insight %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("insight-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'SYSTEM',$2,'2026-08-24T07:00:00+07:00',$3,'PROCESSED') RETURNING id`, householdID, fmt.Sprintf("insight-%d", stamp), []byte(fmt.Sprintf("insight-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,created_by_user_id,confirmed_at) VALUES($1,'INCOME','CONFIRMED',10000000,'2026-08-24T07:00:00+07:00','Gaji',$2,now()) RETURNING id`, householdID, userID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO salary_source(household_id,user_id,employer,normalized_employer,is_primary) VALUES($1,$2,'Test Employer','test employer',true) RETURNING id`, householdID, userID).Scan(&salarySourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO salary_event(salary_source_id,household_id,payroll_period,pay_date,net_pay,transaction_id,status,source_event_id) VALUES($1,$2,'2026-08-01','2026-08-24',10000000,$3,'CONFIRMED',$4)`, salarySourceID, householdID, transactionID, sourceID); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(pool)
	handler.now = func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.FixedZone("UTC+7", 7*60*60)) }
	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/insights/generate?period=cycle", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	handler.Generate(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var generated map[string]string
	if json.Unmarshal(response.Body.Bytes(), &generated) != nil || generated["id"] == "" {
		t.Fatalf("response=%s", response.Body.String())
	}

	var period time.Time
	var kind, start string
	if err := pool.QueryRow(ctx, `SELECT period,input_metrics_json->>'period_kind',input_metrics_json->>'period_start' FROM insight WHERE id=$1`, generated["id"]).Scan(&period, &kind, &start); err != nil {
		t.Fatal(err)
	}
	if period.Format("2006-01-02") != "2026-08-24" || kind != "CURRENT_CYCLE" || start != "2026-08-24" {
		t.Fatalf("period=%s kind=%s start=%s", period.Format("2006-01-02"), kind, start)
	}
	var jobCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='GENERATE_INSIGHT' AND payload_json->>'insight_id'=$1`, generated["id"]).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("jobs=%d err=%v", jobCount, err)
	}

	retry := httptest.NewRecorder()
	handler.Generate(retry, request)
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	if _, err := pool.Exec(ctx, `UPDATE insight SET status='FAILED' WHERE id=$1`, generated["id"]); err != nil {
		t.Fatal(err)
	}
	retryFailed := httptest.NewRecorder()
	handler.Generate(retryFailed, request)
	if retryFailed.Code != http.StatusAccepted {
		t.Fatalf("failed retry status=%d body=%s", retryFailed.Code, retryFailed.Body.String())
	}
	// More than twelve newer rows must not hide the selected older cycle. Large
	// audit records stay stored, but are excluded from the browser projection.
	if _, err := pool.Exec(ctx, `UPDATE insight SET input_metrics_json=input_metrics_json || '{"facts_snapshot":{"private":"fixture"},"tool_reads":[{"private":"fixture"}]}'::jsonb WHERE id=$1`, generated["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO insight(household_id,period,status,input_metrics_json,prompt_version,data_completeness,created_at,requested_by_user_id,completed_at) SELECT $1,'2026-09-01','SUCCEEDED','{"period_start":"2026-09-01","period_kind":"SALARY_CYCLE"}', 'cycle-analyst-v3',1,now()+n*interval '1 minute',$2,now() FROM generate_series(1,14) n`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	listRequest := httptest.NewRequest("GET", "/api/v1/insights?cycle_start=2026-08-24", nil)
	listRequest = listRequest.WithContext(auth.ContextWithPrincipal(listRequest.Context(), principal))
	listed := httptest.NewRecorder()
	handler.List(listed, listRequest)
	if listed.Code != 200 || listed.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("list status=%d cache=%s", listed.Code, listed.Header().Get("Cache-Control"))
	}
	var items []struct {
		ID      string         `json:"id"`
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &items); err != nil || len(items) != 2 {
		t.Fatalf("older cycle count=%d err=%v", len(items), err)
	}
	for _, item := range items {
		if item.Metrics["period_start"] != "2026-08-24" || item.Metrics["facts_snapshot"] != nil || item.Metrics["tool_reads"] != nil {
			t.Fatal("presentation leaked audit data or another cycle")
		}
	}
	var preserved bool
	if err := pool.QueryRow(ctx, `SELECT input_metrics_json ? 'facts_snapshot' AND input_metrics_json ? 'tool_reads' FROM insight WHERE id=$1`, generated["id"]).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("audit preservation=%t err=%v", preserved, err)
	}
	var otherHousehold string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Other synthetic household') RETURNING id`).Scan(&otherHousehold); err != nil {
		t.Fatal(err)
	}
	foreign := auth.Principal{HouseholdID: otherHousehold, HasHousehold: true}
	listRequest = listRequest.WithContext(auth.ContextWithPrincipal(listRequest.Context(), foreign))
	isolated := httptest.NewRecorder()
	handler.List(isolated, listRequest)
	if isolated.Code != 200 || strings.TrimSpace(isolated.Body.String()) != "[]" {
		t.Fatalf("foreign list status=%d body=%s", isolated.Code, isolated.Body.String())
	}
}
