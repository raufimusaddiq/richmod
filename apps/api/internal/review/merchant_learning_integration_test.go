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

func TestConfirmOnlyLearnsMerchantWhenExplicitlyRequested(t *testing.T) {
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
	var merchantID string
	if err := pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, householdID, fmt.Sprintf("Merchant %d", stamp)).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	handler := NewHandler(pool)

	confirm := func(remember bool) string {
		t.Helper()
		var transactionID string
		if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,category_id,merchant_id) VALUES($1,'EXPENSE','NEEDS_REVIEW',125000,now(),$2,$3) RETURNING id`, householdID, categoryID, merchantID).Scan(&transactionID); err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"categoryId":%q,"rememberMerchant":%t}`, categoryID, remember))
		request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transactionID+"/confirm", bytes.NewReader(body))
		request.SetPathValue("id", transactionID)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
		response := httptest.NewRecorder()
		handler.Confirm(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("remember=%t status=%d body=%s", remember, response.Code, response.Body.String())
		}
		return transactionID
	}

	confirm(false)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM merchant_alias WHERE household_id=$1`, householdID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("ordinary confirmation created %d aliases: %v", count, err)
	}

	transactionID := confirm(true)
	var aliasHousehold, aliasCategory string
	var autoApply, confirmed bool
	if err := pool.QueryRow(ctx, `SELECT household_id,default_category_id,auto_apply,created_from_user_confirmation FROM merchant_alias WHERE normalized_merchant_id=$1`, merchantID).Scan(&aliasHousehold, &aliasCategory, &autoApply, &confirmed); err != nil {
		t.Fatal(err)
	}
	if aliasHousehold != householdID || aliasCategory != categoryID || !autoApply || !confirmed {
		t.Fatalf("unexpected alias household=%s category=%s auto=%t confirmed=%t", aliasHousehold, aliasCategory, autoApply, confirmed)
	}
	var audited bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_log WHERE household_id=$1 AND entity_id=$2 AND action='CONFIRM_REVIEW' AND after_json->>'remember_merchant'='true')`, householdID, transactionID).Scan(&audited); err != nil || !audited {
		t.Fatalf("explicit learning audit missing: %v", err)
	}
}

func TestBankReviewRequiresMerchantBeforeConfirmation(t *testing.T) {
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
	var sourceID, transactionID string
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, householdID, fmt.Sprintf("bank-review-%d", stamp), []byte(fmt.Sprintf("bank-review-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',13120,now()) RETURNING id`, householdID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'BANK_EMAIL')`, transactionID, sourceID); err != nil {
		t.Fatal(err)
	}

	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	handler := NewHandler(pool)
	confirm := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+transactionID+"/confirm", bytes.NewBufferString(body))
		request.SetPathValue("id", transactionID)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
		response := httptest.NewRecorder()
		handler.Confirm(response, request)
		return response
	}

	missing := confirm(fmt.Sprintf(`{"categoryId":%q}`, categoryID))
	if missing.Code != http.StatusBadRequest || !bytes.Contains(missing.Body.Bytes(), []byte("merchant is required")) {
		t.Fatalf("missing merchant status=%d body=%s", missing.Code, missing.Body.String())
	}
	var status string
	var merchantID *string
	if err := pool.QueryRow(ctx, `SELECT status,merchant_id::text FROM transaction WHERE id=$1`, transactionID).Scan(&status, &merchantID); err != nil {
		t.Fatal(err)
	}
	if status != "NEEDS_REVIEW" || merchantID != nil {
		t.Fatalf("failed confirmation mutated transaction status=%s merchant=%v", status, merchantID)
	}

	accepted := confirm(fmt.Sprintf(`{"categoryId":%q,"merchantName":"Warung Gorengan"}`, categoryID))
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("accepted status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var merchantName string
	if err := pool.QueryRow(ctx, `SELECT t.status,m.normalized_name FROM transaction t JOIN merchant m ON m.id=t.merchant_id WHERE t.id=$1`, transactionID).Scan(&status, &merchantName); err != nil {
		t.Fatal(err)
	}
	if status != "CONFIRMED" || merchantName != "Warung Gorengan" {
		t.Fatalf("confirmed status=%s merchant=%q", status, merchantName)
	}
}
