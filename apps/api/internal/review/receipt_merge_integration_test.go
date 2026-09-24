package review

import (
	"bytes"
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

func TestReceiptDuplicateCandidateMergesThroughReviewAction(t *testing.T) {
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
	var sourceID, receiptID, targetID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_IMAGE',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("receipt-merge-%d", stamp), []byte(fmt.Sprintf("receipt-merge-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description) VALUES($1,'EXPENSE','NEEDS_REVIEW',57500,'IDR',$2,$3,'Receipt') RETURNING id`, householdID, at, categoryID).Scan(&receiptID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,category_id,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',57500,'IDR',$2,$3,'Existing transaction',now()) RETURNING id`, householdID, at.Add(-30*time.Minute), categoryID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'RECEIPT_IMAGE','{}')`, receiptID, sourceID); err != nil {
		t.Fatal(err)
	}
	decision, _ := json.Marshal(map[string]any{"version": 1, "reasonCode": "POSSIBLE_DUPLICATE", "allowedActions": []string{"MERGE_EXISTING", "CONFIRM_REVIEW", "IGNORE"}})
	if _, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status,decision) VALUES($1,$2,'POSSIBLE_DUPLICATE','OPEN',$3)`, householdID, receiptID, decision); err != nil {
		t.Fatal(err)
	}
	items := listTransactionReviews(t, pool, householdID, userID)
	found := false
	for _, item := range items {
		if item.ID == receiptID {
			found = len(item.Candidates) == 1 && item.Candidates[0].ID == targetID && len(item.AllowedActions) == 3 && item.AllowedActions[0] == "MERGE_EXISTING" && item.AllowedActions[1] == "CONFIRM_REVIEW" && item.AllowedActions[2] == "IGNORE"
			break
		}
	}
	if !found {
		t.Fatal("receipt duplicate did not expose the candidate and supported actions to the Inbox")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+receiptID+"/merge", bytes.NewBufferString(fmt.Sprintf(`{"targetTransactionId":%q}`, targetID)))
	request.SetPathValue("id", receiptID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	NewHandler(pool).Merge(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("receipt candidate merge status=%d body=%s", response.Code, response.Body.String())
	}

	var sourceStatus, targetStatus, reviewStatus, resolutionAction, eventStatus string
	var evidenceCount int
	if err := pool.QueryRow(ctx, `SELECT (SELECT status FROM transaction WHERE id=$1),(SELECT status FROM transaction WHERE id=$2),(SELECT status FROM review_item WHERE transaction_id=$1),(SELECT resolution_action FROM review_item WHERE transaction_id=$1),(SELECT processing_status FROM source_event WHERE id=$3),(SELECT count(*) FROM transaction_evidence WHERE transaction_id=$2 AND source_event_id=$3)`, receiptID, targetID, sourceID).Scan(&sourceStatus, &targetStatus, &reviewStatus, &resolutionAction, &eventStatus, &evidenceCount); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "VOIDED" || targetStatus != "CONFIRMED" || reviewStatus != "RESOLVED" || resolutionAction != "MERGE_REVIEW" || eventStatus != "PROCESSED" || evidenceCount != 1 {
		t.Fatalf("receipt merge did not preserve one canonical transaction and finalize evidence: source=%s target=%s review=%s action=%s event=%s evidence=%d", sourceStatus, targetStatus, reviewStatus, resolutionAction, eventStatus, evidenceCount)
	}
}
