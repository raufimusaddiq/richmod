package ledger

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

func TestTransactionListFiltersAndProvenance(t *testing.T) {
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
	var householdID, userID, accountID, categoryID, merchantID, sourceID, transactionID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Ledger filters %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Raufi Test','unused') RETURNING id`, fmt.Sprintf("ledger-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Primary bank','BANK','SPENDING_ONLY') RETURNING id`, householdID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Groceries','groceries') RETURNING id`, householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAMELLA DUA') RETURNING id`, householdID).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'PROCESSED') RETURNING id`, householdID, fmt.Sprintf("ledger-source-%d", stamp), []byte(fmt.Sprintf("ledger-%d", stamp))).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO transaction(household_id,account_id,type,status,amount,transaction_at,merchant_id,category_id,description,created_by_user_id,confirmed_at) VALUES($1,$2,'EXPENSE','CONFIRMED',55199,now(),$3,$4,'Belanja bulanan',$5,now()) RETURNING id`, householdID, accountID, merchantID, categoryID, userID).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type) VALUES($1,$2,'BANK_EMAIL')`, transactionID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'CONFIRM','transaction',$3,'{}')`, householdID, userID, transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,now(),'Gaji',now())`, householdID); err != nil {
		t.Fatal(err)
	}

	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/transactions?type=EXPENSE&categoryId=%s&memberId=%s&accountId=%s&source=BANK_EMAIL&q=pamella", categoryID, userID, accountID), nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	NewHandler(pool).ListTransactions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var items []transactionView
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != transactionID || value(items[0].CategoryName) != "Groceries" || value(items[0].MerchantName) != "PAMELLA DUA" || value(items[0].AccountName) != "Primary bank" || value(items[0].MemberName) != "Raufi Test" || value(items[0].SourceType) != "BANK_EMAIL" {
		t.Fatalf("unexpected items: %#v", items)
	}

	auditRequest := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/"+transactionID+"/audit", nil)
	auditRequest.SetPathValue("id", transactionID)
	auditRequest = auditRequest.WithContext(auth.ContextWithPrincipal(auditRequest.Context(), principal))
	auditResponse := httptest.NewRecorder()
	NewHandler(pool).Audit(auditResponse, auditRequest)
	if auditResponse.Code != http.StatusOK || !json.Valid(auditResponse.Body.Bytes()) {
		t.Fatalf("audit status=%d body=%s", auditResponse.Code, auditResponse.Body.String())
	}
}

func value(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}
