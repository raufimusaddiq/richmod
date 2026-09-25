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

func TestLoadMerchantMemoryRequiresOneUnambiguousNormalizedRule(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	var householdID, categoryID, merchantID string
	stamp := time.Now().UnixNano()
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Merchant memory %d", stamp)).Scan(&householdID); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND active LIMIT 1`, householdID).Scan(&categoryID); err == nil {
		t.Fatal("new household unexpectedly has a category")
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,$2,$3) RETURNING id`, householdID, "Food", fmt.Sprintf("food-%d", stamp)).Scan(&categoryID); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, householdID, "Cafe").Scan(&merchantID); err != nil { t.Fatal(err) }
	if _, err = pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'Cafe',$2,$3,true,true)`, householdID, merchantID, categoryID); err != nil { t.Fatal(err) }
	memory, err := loadMerchantMemory(ctx, pool, householdID, " cafe " )
	if err != nil || !memory.AutoApply || memory.CategoryID != categoryID { t.Fatalf("expected normalized learned match: %+v, %v", memory, err) }
	if _, err = pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,' CAFE  ',$2,$3,true,true)`, householdID, merchantID, categoryID); err == nil { t.Fatal("normalized duplicate alias was accepted") }
}
