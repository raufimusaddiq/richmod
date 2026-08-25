package gmail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/jago"
)

func TestDisabledMerchantRuleStopsAutoApplication(t *testing.T) {
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
	var householdID, categoryID, aliasID string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Disabled rule %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Groceries','groceries') RETURNING id`, householdID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `WITH m AS (INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAMELLA DUA') RETURNING id) INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) SELECT $1,'PAMELLA DUA',id,$2,true,true FROM m RETURNING id`, householdID, categoryID).Scan(&aliasID); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool}
	event := jago.Event{Merchant: "PAMELLA DUA"}
	learned, confidence, err := processor.classifyCategory(ctx, householdID, "unused-source", event)
	if err != nil || learned == nil || *learned != categoryID || confidence != 1 {
		t.Fatalf("enabled rule category=%v confidence=%v err=%v", learned, confidence, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE merchant_alias SET auto_apply=false WHERE id=$1`, aliasID); err != nil {
		t.Fatal(err)
	}
	learned, _, err = processor.classifyCategory(ctx, householdID, "unused-source", event)
	if err == nil || learned != nil {
		t.Fatalf("disabled rule applied category=%v err=%v", learned, err)
	}
}
