package document

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScreenshotDifferentPrintedTimeCreatesSeparateTransaction(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Screenshot time %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool}
	for _, tt := range []struct {
		name     string
		hours    float64
		merchant string
		linked   bool
	}{
		{"same merchant near time links", 0.25, "Calorie Snacks & Desserts", true},
		{"same merchant different time links nothing", 5.4, "Calorie Snacks & Desserts", false},
		{"different merchant 12h links nothing", 12, "Other Merchant", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var merchantID string
			if err := pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) ON CONFLICT(household_id,(lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')))) DO UPDATE SET normalized_name=EXCLUDED.normalized_name RETURNING id::text`, householdID, tt.merchant).Scan(&merchantID); err != nil {
				t.Fatal(err)
			}
			var id string
			if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,merchant_id,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',7500,'IDR',now() - make_interval(secs => $2::float8),$3,'Calorie Snacks & Desserts',now()) RETURNING id::text`, householdID, tt.hours*3600, merchantID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			matches, err := processor.findScreenshotMatches(ctx, householdID, "EXPENSE", "7500", time.Now(), "Calorie Snacks & Desserts", true)
			if err != nil {
				t.Fatal(err)
			}
			linked := false
			for _, candidate := range matches {
				linked = linked || candidate.ID == id && candidate.Score >= .90
			}
			if linked != tt.linked {
				t.Fatalf("linked=%v want=%v matches=%v", linked, tt.linked, matches)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM transaction WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
		})
	}
}
