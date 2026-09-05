package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseAllowsOnlyOneActiveHouseholdPerUser(t *testing.T) {
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
	var householdA, householdB, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Invariant A %d", stamp)).Scan(&householdA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Invariant B %d", stamp)).Scan(&householdB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Invariant User','!') RETURNING id`, fmt.Sprintf("invariant-%d@example.test", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdA, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdB, userID); err == nil {
		t.Fatal("second active household membership succeeded")
	} else if pgErr, ok := err.(*pgconn.PgError); !ok || pgErr.Code != "23505" || pgErr.ConstraintName != "household_member_one_active_household_per_user" {
		t.Fatalf("second active membership error = %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=false,deactivated_at=now() WHERE household_id=$1 AND user_id=$2`, householdA, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdB, userID); err != nil {
		t.Fatalf("inactive history plus one active membership failed: %v", err)
	}
}
