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
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/transactions?type=EXPENSE&categoryId=%s&memberId=%s&merchantId=%s&accountId=%s&source=BANK_EMAIL&q=pamella", categoryID, userID, merchantID, accountID), nil)
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

	// Review drill-down uses exact IDs, includes refunds, excludes next-cycle
	// midnight and never includes another household's merchant.
	var similarMerchant, foreignHousehold, foreignMerchant string
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAMELLA DUA LAIN') RETURNING id`, householdID).Scan(&similarMerchant); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('Foreign review drill-down') RETURNING id`).Scan(&foreignHousehold); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAMELLA DUA') RETURNING id`, foreignHousehold).Scan(&foreignMerchant); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ household, merchant, category, typ, status, at string }{
		{householdID, merchantID, categoryID, "EXPENSE", "CONFIRMED", "2026-08-26T00:00:00+07:00"},
		{householdID, merchantID, categoryID, "REFUND", "CONFIRMED", "2026-09-24T23:59:59+07:00"},
		{householdID, merchantID, categoryID, "EXPENSE", "CONFIRMED", "2026-08-25T23:59:59+07:00"},
		{householdID, merchantID, categoryID, "EXPENSE", "CONFIRMED", "2026-09-25T00:00:00+07:00"},
		{householdID, merchantID, categoryID, "INCOME", "CONFIRMED", "2026-09-10T08:00:00+07:00"},
		{householdID, merchantID, categoryID, "EXPENSE", "NEEDS_REVIEW", "2026-09-10T08:00:00+07:00"},
		{householdID, similarMerchant, categoryID, "EXPENSE", "CONFIRMED", "2026-09-10T08:00:00+07:00"},
		{householdID, "", "", "EXPENSE", "CONFIRMED", "2026-09-10T08:00:00+07:00"},
		{foreignHousehold, foreignMerchant, "", "EXPENSE", "CONFIRMED", "2026-09-10T08:00:00+07:00"},
	} {
		if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,merchant_id,category_id,type,status,amount,transaction_at,confirmed_at) VALUES($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,$5,1000,$6::timestamptz,CASE WHEN $5='CONFIRMED' THEN now() END)`, row.household, row.merchant, row.category, row.typ, row.status, row.at); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct{ filter string; count int }{
		{"merchantId=" + merchantID + "&categoryId=" + categoryID, 2},
		{"categoryId=uncategorized", 1},
		{"merchantId=" + foreignMerchant, 0},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?from=2026-08-26&to=2026-09-24&type=SPENDING&status=CONFIRMED&"+check.filter, nil)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		NewHandler(pool).ListTransactions(w, r)
		var matches []transactionView
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &matches) != nil || len(matches) != check.count {
			t.Fatalf("review filter=%s status=%d body=%s", check.filter, w.Code, w.Body.String())
		}
	}

	pageOne := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?limit=1", nil).WithContext(auth.ContextWithPrincipal(request.Context(), principal))
	pageOneResponse := httptest.NewRecorder()
	NewHandler(pool).ListTransactions(pageOneResponse, pageOne)
	if pageOneResponse.Code != http.StatusOK || pageOneResponse.Header().Get("X-Next-Cursor") == "" {
		t.Fatalf("first page status=%d cursor=%q", pageOneResponse.Code, pageOneResponse.Header().Get("X-Next-Cursor"))
	}
	var pageOneItems []transactionView
	if err := json.Unmarshal(pageOneResponse.Body.Bytes(), &pageOneItems); err != nil || len(pageOneItems) != 1 {
		t.Fatalf("first page body=%s", pageOneResponse.Body.String())
	}
	pageTwo := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?limit=1&cursor="+pageOneResponse.Header().Get("X-Next-Cursor"), nil).WithContext(auth.ContextWithPrincipal(request.Context(), principal))
	pageTwoResponse := httptest.NewRecorder()
	NewHandler(pool).ListTransactions(pageTwoResponse, pageTwo)
	var pageTwoItems []transactionView
	if pageTwoResponse.Code != http.StatusOK || json.Unmarshal(pageTwoResponse.Body.Bytes(), &pageTwoItems) != nil || len(pageTwoItems) != 1 || pageTwoItems[0].ID == pageOneItems[0].ID {
		t.Fatalf("second page status=%d body=%s", pageTwoResponse.Code, pageTwoResponse.Body.String())
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
