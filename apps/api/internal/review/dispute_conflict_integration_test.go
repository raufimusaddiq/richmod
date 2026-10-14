package review

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// A staged screenshot-link dispute is resolved only by its Telegram buttons.
// Web confirm/merge must answer an explicit 409 and mutate nothing.
func TestWebConfirmAndMergeOnStagedDisputeAreExplicitConflictsWithoutMutation(t *testing.T) {
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
	householdID, userID, categoryID := seedTransferReviewOwner(t, pool, stamp)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var sourceID, staged, target string
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'PROCESSED') RETURNING id::text`, householdID, "dispute-web-"+strings.Repeat("x", 4)+time.Now().Format("150405.000000000"), []byte("dispute-web")).Scan(&sourceID))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',7500,'IDR',now(),$2) RETURNING id::text`, householdID, categoryID).Scan(&staged))
	must(pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',7500,'IDR',now(),now()) RETURNING id::text`, householdID).Scan(&target))
	_, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY','{"evidence_link_dispute":true}')`, staged, sourceID)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN','{"version":1,"reasonCode":"POSSIBLE_DUPLICATE","allowedActions":["MERGE_EXISTING","CONFIRM_REVIEW","IGNORE"],"decisionProvenance":{"evidence_link_dispute":true}}'::jsonb)`, householdID, staged)
	must(err)
	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}

	call := func(path, body string, run func(*Handler, http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.SetPathValue("id", staged)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
		response := httptest.NewRecorder()
		run(NewHandler(pool), response, request)
		return response
	}
	confirm := call("/api/v1/reviews/"+staged+"/confirm", `{}`, (*Handler).Confirm)
	merge := call("/api/v1/reviews/"+staged+"/merge", `{"targetTransactionId":"`+target+`"}`, (*Handler).Merge)
	for name, response := range map[string]*httptest.ResponseRecorder{"confirm": confirm, "merge": merge} {
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Telegram") {
			t.Fatalf("%s: status=%d body=%s, want an explicit 409 naming Telegram", name, response.Code, response.Body.String())
		}
	}
	var status string
	var merges int
	must(pool.QueryRow(ctx, `SELECT status FROM transaction WHERE id=$1`, staged).Scan(&status))
	must(pool.QueryRow(ctx, `SELECT count(*) FROM reconciliation_merge WHERE household_id=$1`, householdID).Scan(&merges))
	if status != "NEEDS_REVIEW" || merges != 0 {
		t.Fatalf("web refusal mutated state: status=%s merges=%d", status, merges)
	}
}
