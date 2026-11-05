package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDatabaseBindsEachUserToOneHousehold: a user belongs to exactly one
// household for good (migration 00088): neither a second active membership
// nor one after deactivation is accepted.
func TestDatabaseBindsEachUserToOneHousehold(t *testing.T) {
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
	} else if pgErr, ok := err.(*pgconn.PgError); !ok || pgErr.Code != "23505" {
		t.Fatalf("second active membership error = %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE household_member SET active=false,deactivated_at=now() WHERE household_id=$1 AND user_id=$2`, householdA, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, householdB, userID); err == nil {
		t.Fatal("a removed member joined another household")
	} else if pgErr, ok := err.(*pgconn.PgError); !ok || pgErr.Code != "23505" || pgErr.ConstraintName != "household_member_one_household_per_user" {
		t.Fatalf("membership after removal error = %v", err)
	}
}

// TestDatabaseBindsTelegramIDAndUserOneToOne: one Telegram ID per user, one
// user per Telegram ID, and the identity must be its own user's membership.
func TestDatabaseBindsTelegramIDAndUserOneToOne(t *testing.T) {
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
	var householdA, householdB, userA, userB string
	for _, target := range []*string{&householdA, &householdB} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Binding %d", stamp)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for i, target := range []*string{&userA, &userB} {
		if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Binding','!') RETURNING id`, fmt.Sprintf("binding-%d-%d@example.test", stamp, i)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER'),($3,$4,'MEMBER')`, householdA, userA, householdB, userB); err != nil {
		t.Fatal(err)
	}
	firstID, secondID := stamp%9_000_000_000+41, stamp%9_000_000_000+43
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, firstID, householdA, userA); err != nil {
		t.Fatal(err)
	}
	// Any of the named constraints may report a violation first.
	expect := func(label, constraints, query string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, query, args...)
		pgErr, ok := err.(*pgconn.PgError)
		if !ok || !strings.Contains(" "+constraints+" ", " "+pgErr.ConstraintName+" ") {
			t.Fatalf("%s: err=%v, want violation of %s", label, err, constraints)
		}
	}
	// telegram_identity_one_per_user alone enforces this; the implied
	// (household_id, user_id) unique key was dropped in migration 00089.
	expect("second Telegram ID for one user", "telegram_identity_one_per_user",
		`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, secondID, householdA, userA)
	expect("one Telegram ID for a second user", "telegram_identity_pkey",
		`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, firstID, householdB, userB)
	expect("identity outside the user's household", "telegram_identity_membership_fkey",
		`INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, secondID, householdA, userB)
}
