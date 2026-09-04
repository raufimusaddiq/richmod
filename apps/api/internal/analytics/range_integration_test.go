package analytics

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
	"github.com/raufimusaddiq/richmod/apps/api/internal/clock"
)

func TestCashflowRangeIncludesOnlyConfirmedFinancialState(t *testing.T) {
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
	var householdID, userID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Analytics %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Analytics Owner','unused') RETURNING id`, fmt.Sprintf("analytics-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,'INCOME','CONFIRMED',1000000,'2026-08-10 10:00+07','Income',now()),($1,'EXPENSE','CONFIRMED',250000,'2026-08-11 10:00+07','Expense',now()),($1,'REFUND','CONFIRMED',50000,'2026-08-12 10:00+07','Refund',now()),($1,'TRANSFER','CONFIRMED',999999,'2026-08-13 10:00+07','Transfer',now()),($1,'EXPENSE','NEEDS_REVIEW',777777,'2026-08-14 10:00+07','Unresolved',NULL)`, householdID); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool)
	handler.now = func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, clock.HouseholdLocation()) }
	request := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cashflow?range=3", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Memberships: []auth.Membership{{HouseholdID: householdID, Role: "OWNER"}}}))
	response := httptest.NewRecorder()
	handler.Cashflow(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var rows []monthlyValue
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[2].Income != "1000000" || rows[2].Expense != "200000" || rows[2].Refund != "50000" || rows[2].Net != "800000" {
		t.Fatalf("rows=%#v", rows)
	}
}
