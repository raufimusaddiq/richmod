package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestWriteSenderWriteErrorSeparatesDuplicatesFromRealErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"bank listener duplicate", &pgconn.PgError{Code: "23505", ConstraintName: bankListenerSenderUnique}, 409},
		{"route duplicate", &pgconn.PgError{Code: "23505", ConstraintName: senderRouteActiveUnique}, 409},
		{"other unique violation", &pgconn.PgError{Code: "23505", ConstraintName: "account_name_unique"}, 500},
		{"foreign key violation", &pgconn.PgError{Code: "23503", ConstraintName: senderRouteActiveUnique}, 500},
		{"connection failure", errors.New("connection reset"), 500},
	} {
		response := httptest.NewRecorder()
		writeSenderWriteError(response, tc.err, "unable to save")
		if response.Code != tc.status {
			t.Errorf("%s: status %d want %d", tc.name, response.Code, tc.status)
		}
	}
}

func TestDuplicateBankListenerSenderIs409(t *testing.T) {
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
	var household, user string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Duplicate sender %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','x') RETURNING id`, fmt.Sprintf("duplicate-sender-%d@test.invalid", stamp)).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: user, Memberships: []auth.Membership{{HouseholdID: household, Role: "OWNER"}}}
	handler := NewHandler(pool)
	post := func(bank string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/bank-email-listeners", bytes.NewBufferString(fmt.Sprintf(`{"bankName":%q,"senderAddress":"alerts@dup.test.invalid"}`, bank)))
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		handler.BankEmailListeners(w, r)
		return w.Code
	}
	if code := post("First Bank"); code != http.StatusCreated {
		t.Fatalf("first create = %d", code)
	}
	if code := post("Second Bank"); code != http.StatusConflict {
		t.Fatalf("duplicate sender = %d, want 409", code)
	}
}
