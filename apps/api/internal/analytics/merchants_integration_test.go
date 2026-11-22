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
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/clock"
)

func TestMerchantRankingOrdersByNumericAmountAndGroupsCounterparties(t *testing.T) {
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
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Merchants %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Merchant Owner','unused') RETURNING id`, fmt.Sprintf("merchants-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}
	var parents, rent string
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Orang Tua Payee') RETURNING id`, householdID).Scan(&parents); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'Rent Payee') RETURNING id`, householdID).Scan(&rent); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id) VALUES($1,'ORANG  TUA payee',$2)`, householdID, parents); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,purpose,counterparty_name) VALUES($1,'EXPENSE','CONFIRMED',$2,'2026-08-10 10:00+07','small',now(),'GENERAL',$3)`, householdID, 96900+i, fmt.Sprintf("Small %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,purpose,counterparty_name) VALUES($1,'EXPENSE','CONFIRMED',799240,'2026-08-11 10:00+07','grab',now(),'GENERAL','GRAB')`, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,purpose,merchant_id) VALUES($1,'EXPENSE','CONFIRMED',5850000,'2026-08-12 10:00+07','rent',now(),'GENERAL',$2)`, householdID, rent); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,purpose,merchant_id) VALUES($1,'EXPENSE','CONFIRMED',3000000,'2026-07-12 10:00+07','parents old',now(),'GENERAL',$2)`, householdID, parents); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,purpose,counterparty_name) VALUES($1,'EXPENSE','CONFIRMED',2500000,'2026-08-13 10:00+07','parents email',now(),'GENERAL','orang tua  PAYEE')`, householdID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Warung  Baru", "warung baru"} {
		if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,purpose,counterparty_name) VALUES($1,'EXPENSE','CONFIRMED',1000000,'2026-08-14 10:00+07','warung',now(),'GENERAL',$2)`, householdID, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at,purpose) VALUES($1,'EXPENSE','CONFIRMED',1586492,'2026-08-15 10:00+07','anon',now(),'GENERAL')`, householdID); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool)
	handler.now = func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, clock.HouseholdLocation()) }
	request := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/merchants?period=calendar&range=6", nil)
	request = request.WithContext(auth.ContextWithPrincipal(request.Context(), auth.Principal{UserID: userID, Household: &auth.Membership{HouseholdID: householdID, Role: "OWNER"}}))
	response := httptest.NewRecorder()
	handler.Merchants(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var rows []rankedValue
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, amount string }{
		{"Rent Payee", "5850000"},
		{"Orang Tua Payee", "5500000"},
		{"Warung  Baru", "2000000"},
		{"Tanpa merchant", "1586492"},
		{"GRAB", "799240"},
	}
	if len(rows) != 10 {
		t.Fatalf("rows=%#v", rows)
	}
	for i, w := range want {
		if rows[i].Name != w.name && !(w.name == "Warung  Baru" && rows[i].Name == "warung baru") {
			if rows[i].Name != "Warung Baru" && rows[i].Name != "warung baru" {
				t.Fatalf("row %d name=%q want %q; rows=%#v", i, rows[i].Name, w.name, rows)
			}
		}
		if rows[i].Amount != w.amount {
			t.Fatalf("row %d amount=%q want %q; rows=%#v", i, rows[i].Amount, w.amount, rows)
		}
	}
	if rows[1].ID == nil || *rows[1].ID != parents {
		t.Fatalf("parents row should carry merchant id: %#v", rows[1])
	}
	if rows[3].ID != nil {
		t.Fatalf("Tanpa merchant row must not carry an id: %#v", rows[3])
	}
}
