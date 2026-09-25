package review

import (
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

// The shared reject operation must void the transaction, reject its proposal,
// cancel the Telegram projection, and resolve the canonical item in one step,
// while refusing a cross-household request.
func TestRejectReviewUsesSharedCanonicalTransition(t *testing.T) {
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
	var proposalID, transactionID, reviewItemID, requestID, sourceID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("reject-%d", stamp), []byte(fmt.Sprintf("reject-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,proposal_status,amount,transaction_at,counterparty_raw,confidence) VALUES($1,$2,'EXPENSE','NEEDS_REVIEW',12000,now(),'Test',0.8) RETURNING id`, householdID, sourceID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',12000,'IDR',now()) RETURNING id`, householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'BANK_EMAIL',jsonb_build_object('proposal_id',$3::uuid))`, transactionID, sourceID, proposalID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_item(household_id,transaction_id,review_type,status) VALUES($1,$2,'UNKNOWN_MERCHANT','OPEN') RETURNING id`, householdID, transactionID).Scan(&reviewItemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO review_request(review_item_id,household_id,transaction_id,review_type,status,telegram_chat_id) VALUES($1,$2,$3,'UNKNOWN_MERCHANT','OPEN',$4) RETURNING id`, reviewItemID, householdID, transactionID, stamp).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transactionID+"/reject", nil)
	request.SetPathValue("id", transactionID)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	NewHandler(pool).Reject(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("reject returned %d: %s", response.Code, response.Body.String())
	}
	var transactionStatus, proposalStatus, requestStatus, itemStatus string
	if err := pool.QueryRow(ctx, `SELECT t.status,p.proposal_status,r.status,i.status FROM transaction t JOIN transaction_proposal p ON p.id=$2 JOIN review_request r ON r.id=$3 JOIN review_item i ON i.id=$4 WHERE t.id=$1`, transactionID, proposalID, requestID, reviewItemID).Scan(&transactionStatus, &proposalStatus, &requestStatus, &itemStatus); err != nil {
		t.Fatal(err)
	}
	if transactionStatus != "VOIDED" || proposalStatus != "REJECTED" || requestStatus != "CANCELLED" || itemStatus != "RESOLVED" {
		t.Fatalf("transaction=%s proposal=%s request=%s item=%s", transactionStatus, proposalStatus, requestStatus, itemStatus)
	}

	// A second household must not be able to reject this transaction.
	foreignHousehold, foreignUser, _ := seedTransferReviewOwner(t, pool, stamp+1)
	_ = foreignHousehold
	foreign := auth.Principal{UserID: foreignUser, Memberships: []auth.Membership{{HouseholdID: foreignHousehold, Role: "OWNER"}}}
	second := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transactionID+"/reject", nil)
	second.SetPathValue("id", transactionID)
	second = second.WithContext(auth.ContextWithPrincipal(second.Context(), foreign))
	secondResponse := httptest.NewRecorder()
	NewHandler(pool).Reject(secondResponse, second)
	if secondResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-household reject must be refused, got %d", secondResponse.Code)
	}
}
