package bankemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResolveMerchantIDTreatsCaseAndWhitespaceAsOneIdentity(t *testing.T) {
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

	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Merchant identity %d", time.Now().UnixNano())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	first, err := resolveMerchantID(ctx, tx, householdID, "Calorie Snacks & Desserts")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveMerchantID(ctx, tx, householdID, "  CALORIE   SNACKS & DESSERTS  ")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("merchant ids differ: %s != %s", first, second)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM merchant WHERE household_id=$1`, householdID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("merchant count=%d", count)
	}
}
