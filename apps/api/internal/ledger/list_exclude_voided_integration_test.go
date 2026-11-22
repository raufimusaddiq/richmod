package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestTransactionListExcludeVoided(t *testing.T) {
	t.Parallel()
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
	stamp := uniqueStamp()
	var householdID, userID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Exclude voided %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Voided Test','unused') RETURNING id`, fmt.Sprintf("voided-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	insert := func(status string, minutesAgo int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,voided_at) VALUES($1,'EXPENSE',$2,1000,now()-($3||' minutes')::interval,'row',CASE WHEN $2='CONFIRMED' THEN now() END,CASE WHEN $2='VOIDED' THEN now() END)`, householdID, status, fmt.Sprint(minutesAgo)); err != nil {
			t.Fatal(err)
		}
	}
	insert("CONFIRMED", 1)
	insert("CONFIRMED", 2)
	insert("VOIDED", 3)
	insert("VOIDED", 4)
	insert("VOIDED", 5)
	principal := auth.Principal{UserID: userID, Household: &auth.Membership{HouseholdID: householdID, Role: "OWNER"}}
	list := func(query string) (int, []transactionView, http.Header) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/transactions"+query, nil)
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		NewHandler(pool).ListTransactions(w, r)
		var items []transactionView
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &items) != nil {
			t.Fatalf("query=%s status=%d body=%s", query, w.Code, w.Body.String())
		}
		return w.Code, items, w.Header()
	}

	_, items, header := list("")
	if len(items) != 5 || header.Get("X-Total-Count") != "5" || header.Get("X-Voided-Count") != "" {
		t.Fatalf("legacy list items=%d total=%q voided=%q", len(items), header.Get("X-Total-Count"), header.Get("X-Voided-Count"))
	}
	_, items, header = list("?exclude_voided=true")
	if len(items) != 2 || header.Get("X-Total-Count") != "2" || header.Get("X-Voided-Count") != "3" {
		t.Fatalf("excluded list items=%d total=%q voided=%q", len(items), header.Get("X-Total-Count"), header.Get("X-Voided-Count"))
	}
	for _, item := range items {
		if item.Status == "VOIDED" {
			t.Fatal("voided row leaked into the excluded list")
		}
	}
	_, items, header = list("?exclude_voided=true&limit=1")
	if len(items) != 1 || header.Get("X-Total-Count") != "2" || header.Get("X-Voided-Count") != "3" || header.Get("X-Next-Cursor") == "" {
		t.Fatalf("paged items=%d total=%q voided=%q", len(items), header.Get("X-Total-Count"), header.Get("X-Voided-Count"))
	}
	_, items, header = list("?exclude_voided=true&status=VOIDED")
	if len(items) != 3 || header.Get("X-Total-Count") != "3" || header.Get("X-Voided-Count") != "" {
		t.Fatalf("status override items=%d total=%q voided=%q", len(items), header.Get("X-Total-Count"), header.Get("X-Voided-Count"))
	}
	_, items, _ = list("?exclude_voided=true")
	cursor := transactionCursor(items[len(items)-1])
	_, items, header = list("?exclude_voided=true&cursor=" + cursor)
	if len(items) != 0 || header.Get("X-Voided-Count") != "3" {
		t.Fatalf("cursor past end items=%d voided=%q", len(items), header.Get("X-Voided-Count"))
	}
	if _, err = pool.Exec(ctx, `UPDATE transaction SET description='only-voided' WHERE household_id=$1 AND status='VOIDED'`, householdID); err != nil {
		t.Fatal(err)
	}
	_, items, header = list("?exclude_voided=true&q=only-voided")
	if len(items) != 0 || header.Get("X-Total-Count") != "0" || header.Get("X-Voided-Count") != "3" {
		t.Fatalf("voided-only match items=%d total=%q voided=%q", len(items), header.Get("X-Total-Count"), header.Get("X-Voided-Count"))
	}
}
