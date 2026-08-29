package review

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestResolveUnknownBankTemplateIgnore(t *testing.T) {
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
	householdID, userID, _ := seedTransferReviewOwner(t, pool, stamp)
	var sourceID, reviewID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("unknown-bank-%d", stamp), []byte(fmt.Sprintf("unknown-bank-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status) VALUES($1,$2,'UNKNOWN_BANK_TEMPLATE','OPEN') RETURNING id`, householdID, sourceID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/resolve", bytes.NewBufferString(`{"action":"IGNORE","values":{}}`))
	request.SetPathValue("id", reviewID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	NewHandler(pool).Resolve(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var sourceStatus, reviewStatus, resolution string
	if err := pool.QueryRow(ctx, `SELECT s.processing_status,ri.status,ri.resolution_action FROM review_item ri JOIN source_event s ON s.id=ri.source_event_id WHERE ri.id=$1`, reviewID).Scan(&sourceStatus, &reviewStatus, &resolution); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "IGNORED" || reviewStatus != "RESOLVED" || resolution != "IGNORE" {
		t.Fatalf("source=%s review=%s resolution=%s", sourceStatus, reviewStatus, resolution)
	}
}
