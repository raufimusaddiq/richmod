package settings

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

func TestFinancialSourceEditChangesAndClearsDefaultWealth(t *testing.T) {
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
	var household, user, wealthA, wealthB, source string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Financial source settings %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','x') RETURNING id`, fmt.Sprintf("financial-source-settings-%d@test.invalid", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	insertWealth := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,institution,side,wealth_type,usage_role) VALUES($1,$2,'Provider','ASSET','BROKERAGE','INVESTMENT') RETURNING id`, household, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	wealthA, wealthB = insertWealth("Target A"), insertWealth("Target B")
	if err = pool.QueryRow(ctx, `INSERT INTO financial_email_source(household_id,provider_name,sender_address,capabilities,default_wealth_account_id,status,created_by_user_id) VALUES($1,'Provider','source-settings@test.invalid',ARRAY['CASH_MOVEMENT'], $2,'DRAFT',$3) RETURNING id`, household, wealthA, user).Scan(&source); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}
	handler := NewHandler(pool)
	patch := func(body string, want int) {
		r := httptest.NewRequest(http.MethodPatch, "/api/v1/financial-email-sources/"+source, bytes.NewBufferString(body))
		r.SetPathValue("id", source)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		handler.FinancialEmailSources(w, r)
		if w.Code != want {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	patch(fmt.Sprintf(`{"defaultWealthAccountId":%q}`, wealthB), http.StatusNoContent)
	var current *string
	var version int
	if err = pool.QueryRow(ctx, `SELECT default_wealth_account_id::text,config_version FROM financial_email_source WHERE id=$1`, source).Scan(&current, &version); err != nil {
		t.Fatal(err)
	}
	if current == nil || *current != wealthB || version != 2 {
		t.Fatalf("default=%v version=%d", current, version)
	}
	patch(`{"defaultWealthAccountId":null}`, http.StatusNoContent)
	if err = pool.QueryRow(ctx, `SELECT default_wealth_account_id::text FROM financial_email_source WHERE id=$1`, source).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != nil {
		t.Fatalf("default was not cleared: %v", *current)
	}
}

func TestAccountAndCategoryLifecyclePreservesRecords(t *testing.T) {
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
	var householdID, userID, accountID, categoryID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Settings lifecycle %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Settings Owner','unused') RETURNING id`, fmt.Sprintf("settings-lifecycle-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Primary bank','BANK','SPENDING_ONLY') RETURNING id`, householdID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Belanja','belanja') RETURNING id`, householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}
	handler := NewHandler(pool)
	call := func(path, id, body string, want int, fn func(http.ResponseWriter, *http.Request)) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPatch, path+id, bytes.NewBufferString(body))
		request.SetPathValue("id", id)
		request = request.WithContext(auth.ContextWithPrincipal(request.Context(), principal))
		response := httptest.NewRecorder()
		fn(response, request)
		if response.Code != want {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	call("/api/v1/accounts/", accountID, `{"trackingPolicy":"FULL_LEDGER"}`, http.StatusConflict, handler.PatchAccount)
	call("/api/v1/accounts/", accountID, `{"active":false}`, http.StatusNoContent, handler.PatchAccount)
	call("/api/v1/categories/", categoryID, `{"name":"Belanja Harian","active":false}`, http.StatusNoContent, handler.PatchCategory)
	var accountActive, categoryActive bool
	var categoryName string
	if err = pool.QueryRow(ctx, `SELECT (SELECT active FROM account WHERE id=$1),(SELECT active FROM category WHERE id=$2),(SELECT name FROM category WHERE id=$2)`, accountID, categoryID).Scan(&accountActive, &categoryActive, &categoryName); err != nil {
		t.Fatal(err)
	}
	if accountActive || categoryActive || categoryName != "Belanja Harian" {
		t.Fatalf("account=%t category=%t name=%s", accountActive, categoryActive, categoryName)
	}
	var audits int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE household_id=$1 AND entity_id IN($2,$3) AND action='UPDATE'`, householdID, accountID, categoryID).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
}
