package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// TestHouseholdDiagnosticsSurfaceComesFromResolution pins the household
// completion split to the resolving surface, not the resolver's identity: a
// Telegram-linked member who resolves on Web counts as Web.
func TestHouseholdDiagnosticsSurfaceComesFromResolution(t *testing.T) {
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
	adminID, memberID, householdID, _ := seedReviewOpsHousehold(t, pool, stamp)
	var sourceID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("hh-surface-%d", stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	resolve := func(actorType string) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		var itemID string
		if err := tx.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY','OPEN','{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb) RETURNING id`, householdID, sourceID).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		tx, err = pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolved_by_user_id=$2,resolution_action='CONFIRM_REVIEW' WHERE id=$1`, itemID, memberID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,$2,$3,'CONFIRM_REVIEW','transaction',gen_random_uuid())`, householdID, actorType, memberID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	resolve("USER")
	resolve("TELEGRAM")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/households/"+householdID+"/overview", nil)
	request.SetPathValue("householdId", householdID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: adminID, IsSuperAdmin: true}))
	response := httptest.NewRecorder()
	NewHandler(pool, false, "responses").Require(http.HandlerFunc(NewHandler(pool, false, "responses").HouseholdOverview)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var parsed struct {
		ReviewDiagnostics struct {
			ResolvedTelegram int `json:"resolvedTelegram"`
			ResolvedWeb      int `json:"resolvedWeb"`
		} `json:"reviewDiagnostics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ReviewDiagnostics.ResolvedWeb != 1 || parsed.ReviewDiagnostics.ResolvedTelegram != 1 {
		t.Fatalf("household surface split = %+v", parsed.ReviewDiagnostics)
	}
}
