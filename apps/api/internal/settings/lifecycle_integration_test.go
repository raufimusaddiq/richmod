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
	call("/api/v1/accounts/", accountID, `{"trackingPolicy":"FULL_LEDGER"}`, http.StatusBadRequest, handler.PatchAccount)
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
