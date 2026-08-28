package ledger

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestTransactionAccessIsHouseholdScopedAndInjectionSafe(t *testing.T) {
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
	password := "integration-password"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var householdA, householdB, userID, ownTransaction, otherTransaction string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Security A %d", stamp)).Scan(&householdA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Security B %d", stamp)).Scan(&householdB); err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("security-%d@example.test", stamp)
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES($1,'Security Test',$2,now()) RETURNING id`, email, hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdA, userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at,description) VALUES($1,'EXPENSE','CONFIRMED',1000,'IDR',now(),now(),'<script>alert(1)</script>') RETURNING id`, householdA).Scan(&ownTransaction); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,confirmed_at,description) VALUES($1,'EXPENSE','CONFIRMED',999999,'IDR',now(),now(),'other household secret marker') RETURNING id`, householdB).Scan(&otherTransaction); err != nil {
		t.Fatal(err)
	}

	authService := auth.NewService(pool)
	_, token, _, err := authService.Login(ctx, email, password)
	if err != nil {
		t.Fatal(err)
	}
	handler := auth.NewHandler(authService, true).RequireSession(http.HandlerFunc(NewHandler(pool).GetTransaction))
	request := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/value", nil)
		req.SetPathValue("id", id)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if response := request(ownTransaction); response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<script>") || !strings.Contains(response.Body.String(), `\u003cscript\u003e`) {
		t.Fatalf("own transaction was not safely JSON-escaped: %d %q", response.Code, response.Body.String())
	}
	if response := request(otherTransaction); response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "secret marker") {
		t.Fatalf("cross-household response = %d %q", response.Code, response.Body.String())
	}
	if response := request("' OR true --"); response.Code == http.StatusOK || strings.Contains(response.Body.String(), "marker") {
		t.Fatalf("injection response = %d %q", response.Code, response.Body.String())
	}
}
