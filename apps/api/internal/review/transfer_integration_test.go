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

func TestClassifyTransferStatesAndHouseholdScope(t *testing.T) {
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
	handler := NewHandler(pool)
	stamp := time.Now().UnixNano()
	householdID, userID, categoryID := seedTransferReviewOwner(t, pool, stamp)
	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	tests := []struct {
		name, classification, wantType, wantStatus, wantProposal, wantSource string
		category                                                             bool
		remember                                                             bool
	}{{"expense", "EXPENSE", "EXPENSE", "CONFIRMED", "ACCEPTED", "PROCESSED", true, false}, {"own account", "OWN_ACCOUNT", "TRANSFER", "CONFIRMED", "ACCEPTED", "PROCESSED", false, true}, {"ignored", "IGNORE", "UNCLASSIFIED", "VOIDED", "REJECTED", "IGNORED", false, false}}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transactionID, sourceID, proposalID := seedUnclassifiedTransfer(t, pool, householdID, stamp+int64(index+1))
			body := map[string]any{"classification": test.classification}
			if test.category {
				body["categoryId"] = categoryID
			}
			if test.remember {
				body["remember"] = true
				body["institution"] = "BCA"
				body["displayName"] = "Rekening sendiri"
				body["matchHint"] = "1234"
			}
			raw, _ := json.Marshal(body)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transactionID+"/classify-transfer", bytes.NewReader(raw))
			request.SetPathValue("id", transactionID)
			request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
			response := httptest.NewRecorder()
			handler.ClassifyTransfer(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var gotType, status, proposalStatus, sourceStatus string
			if err := pool.QueryRow(ctx, `SELECT t.type,t.status,p.proposal_status,s.processing_status FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id JOIN transaction_proposal p ON p.id=$2 JOIN source_event s ON s.id=$3 WHERE t.id=$1`, transactionID, proposalID, sourceID).Scan(&gotType, &status, &proposalStatus, &sourceStatus); err != nil {
				t.Fatal(err)
			}
			if gotType != test.wantType || status != test.wantStatus || proposalStatus != test.wantProposal || sourceStatus != test.wantSource {
				t.Fatalf("got %s/%s proposal=%s source=%s", gotType, status, proposalStatus, sourceStatus)
			}
			if test.remember {
				var relationship string
				if err := pool.QueryRow(ctx, `SELECT relationship FROM known_account WHERE household_id=$1 AND match_hint='1234'`, householdID).Scan(&relationship); err != nil || relationship != "OWN_ACCOUNT" {
					t.Fatalf("relationship=%s err=%v", relationship, err)
				}
			}
		})
	}

	otherHousehold, otherUser, _ := seedTransferReviewOwner(t, pool, stamp+99)
	foreignTransaction, _, _ := seedUnclassifiedTransfer(t, pool, householdID, stamp+100)
	raw, _ := json.Marshal(map[string]any{"classification": "IGNORE"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+foreignTransaction+"/classify-transfer", bytes.NewReader(raw))
	request.SetPathValue("id", foreignTransaction)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: otherUser, Memberships: []auth.Membership{{HouseholdID: otherHousehold, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	handler.ClassifyTransfer(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-household status=%d", response.Code)
	}
}

func seedTransferReviewOwner(t *testing.T, pool *pgxpool.Pool, stamp int64) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	var householdID, userID, categoryID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Review %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("review-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Rumah','rumah') RETURNING id`, householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	return householdID, userID, categoryID
}

func seedUnclassifiedTransfer(t *testing.T, pool *pgxpool.Pool, householdID string, stamp int64) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	var sourceID, proposalID, transactionID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("review-message-%d", stamp), []byte(fmt.Sprintf("review-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,transaction_at,counterparty_raw,confidence,proposal_status) VALUES($1,$2,'UNCLASSIFIED',1000000,now(),'BCA ****1234',.99,'NEEDS_REVIEW') RETURNING id`, householdID, sourceID).Scan(&proposalID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,counterparty_name) VALUES($1,'UNCLASSIFIED','NEEDS_REVIEW',1000000,now(),'BCA ****1234') RETURNING id`, householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'BANK_EMAIL',jsonb_build_object('proposal_id',$3::uuid))`, transactionID, sourceID, proposalID); err != nil {
		t.Fatal(err)
	}
	return transactionID, sourceID, proposalID
}
