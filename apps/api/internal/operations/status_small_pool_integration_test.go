package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestStatusReturnsRealDataWithSmallPool(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	stamp := uniqueStamp()
	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Small pool %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO worker_heartbeat(worker_id,started_at,last_seen_at) VALUES($1,now(),now())`, fmt.Sprintf("w-%d", stamp)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO job(type,household_id,status,payload_json,run_after,updated_at) VALUES('GENERATE_INSIGHT',$1,'PENDING','{}',now(),now())`, householdID); err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),decode(md5($2),'hex'),'PROCESSED') RETURNING id`, householdID, fmt.Sprintf("small-pool-%d", stamp)).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',1000,now(),now())`, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','RESOLVED',now(),'{"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW"]}'::jsonb)`, householdID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO judgment_turn_telemetry(household_id,lane,decision_tasks,policy_version,native_tool_calls_avoided) VALUES($1,'JEV_ONLY','{}','v1',1)`, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO llm_call(household_id,task,protocol,model,status,duration_ms,call_kind) VALUES($1,'TRANSACTION_SEMANTICS','systemone','jev','SUCCEEDED',10,'JUDGMENT')`, householdID); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(pool)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/status", nil)
	request = request.WithContext(auth.ContextWithPrincipal(ctx, auth.Principal{
		UserID:        "00000000-0000-4000-8000-000000000001",
		HasHousehold:  true,
		HouseholdID:   householdID,
		HouseholdRole: "OWNER",
	}))
	response := httptest.NewRecorder()
	handler.Status(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Status   string   `json:"status"`
		Degraded []string `json:"degradedSections"`
		Jobs     struct {
			Pending int `json:"pending"`
		} `json:"jobs"`
		Judgment struct {
			Turns map[string]int `json:"turns"`
		} `json:"judgment"`
		Product struct {
			SourceEvents    int `json:"sourceEvents"`
			CanonicalEvents int `json:"canonicalEvents"`
		} `json:"product"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Jobs.Pending != 1 || body.Judgment.Turns["JEV_ONLY"] != 1 || body.Product.SourceEvents != 1 || body.Product.CanonicalEvents != 1 {
		t.Fatalf("small pool zeroed real data (degraded=%v): %+v", body.Degraded, body)
	}
}
