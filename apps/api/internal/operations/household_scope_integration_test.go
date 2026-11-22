package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestStatusCountsOnlyTheOwnersHouseholdJobs(t *testing.T) {
	t.Parallel()
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := uniqueStamp()
	var own, other string
	for _, target := range []*string{&own, &other} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Ops scope %d", stamp)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO job(type,household_id,status,payload_json,run_after,updated_at) VALUES
		('GENERATE_INSIGHT',$1,'FAILED','{}',now()+interval '1 day',now()),
		('GENERATE_INSIGHT',$1,'PENDING','{}',now()+interval '1 day',now()),
		('GENERATE_INSIGHT',$2,'PENDING','{}',now()+interval '1 day',now())`, other, own); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/status", nil)
	request = request.WithContext(auth.ContextWithPrincipal(ctx, auth.Principal{UserID: "00000000-0000-4000-8000-000000000001", HasHousehold: true, HouseholdID: own, HouseholdRole: "OWNER"}))
	response := httptest.NewRecorder()
	handler := NewHandler(pool)
	handler.probeTimeout, handler.statusTimeout = time.Minute, time.Minute
	handler.Status(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Jobs struct {
			Pending        int                       `json:"pending"`
			RecentFailures int                       `json:"recentFailures"`
			Lanes          map[string]map[string]any `json:"lanes"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Jobs.Pending != 1 || body.Jobs.RecentFailures != 0 {
		t.Fatalf("own household saw pending=%d failures=%d, want 1 and 0", body.Jobs.Pending, body.Jobs.RecentFailures)
	}
	if pending, _ := body.Jobs.Lanes["BACKGROUND"]["pending"].(float64); pending != 1 {
		t.Fatalf("BACKGROUND lane pending = %v, want only the own household's job", body.Jobs.Lanes["BACKGROUND"]["pending"])
	}
}
