package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateAdditionalHousehold(t *testing.T) {
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
	// Guarantee the platform is bootstrapped: one existing owner elsewhere.
	var existingHousehold, existingUser string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Existing %d", stamp)).Scan(&existingHousehold); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Existing','x') RETURNING id`, fmt.Sprintf("existing-%d@example.test", stamp)).Scan(&existingUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, existingHousehold, existingUser); err != nil {
		t.Fatal(err)
	}
	telegramID := stamp % 1_000_000_000_000
	in := householdInput{Email: fmt.Sprintf("second-owner-%d@example.test", stamp), Name: "Second Owner", HouseholdName: fmt.Sprintf("Second %d", stamp), TelegramUserID: telegramID, PasswordHash: "hash"}

	if _, err := createHousehold(ctx, pool, modeFirstOwner, in); !errors.Is(err, errOwnerExists) {
		t.Fatalf("first-owner bootstrap after an owner exists: %v", err)
	}
	householdID, err := createHousehold(ctx, pool, modeAdditionalHousehold, in)
	if err != nil {
		t.Fatal(err)
	}
	var role, linkedHousehold string
	var categories, audits int
	if err := pool.QueryRow(ctx, `SELECT hm.role,ti.household_id::text,
		(SELECT count(*) FROM category WHERE household_id=$1),
		(SELECT count(*) FROM audit_log WHERE household_id=$1 AND action='CREATE_HOUSEHOLD')
		FROM household_member hm JOIN "user" u ON u.id=hm.user_id
		JOIN telegram_identity ti ON ti.user_id=u.id
		WHERE hm.household_id=$1 AND hm.active AND u.email=$2`, householdID, in.Email).Scan(&role, &linkedHousehold, &categories, &audits); err != nil {
		t.Fatal(err)
	}
	if role != "OWNER" || linkedHousehold != householdID || categories == 0 || audits != 1 {
		t.Fatalf("role=%s telegram household=%s categories=%d audits=%d", role, linkedHousehold, categories, audits)
	}
	if householdID == existingHousehold {
		t.Fatal("new household reused an existing one")
	}

	// An existing user is never moved or attached implicitly (ADR-035).
	again := in
	again.HouseholdName = "Third"
	again.TelegramUserID = 0
	if _, err := createHousehold(ctx, pool, modeAdditionalHousehold, again); !errors.Is(err, errUserExists) {
		t.Fatalf("existing email: %v", err)
	}
	reusedTelegram := in
	reusedTelegram.Email = fmt.Sprintf("third-owner-%d@example.test", stamp)
	if _, err := createHousehold(ctx, pool, modeAdditionalHousehold, reusedTelegram); !errors.Is(err, errTelegramIDLinked) {
		t.Fatalf("linked Telegram ID: %v", err)
	}
}
