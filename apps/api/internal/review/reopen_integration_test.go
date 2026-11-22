package review

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
)

func TestReopenTransactionReviewConcurrentReversals(t *testing.T) {
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
	var household, transaction string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Reopen race %d", uniqueStamp())).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,counterparty_name) VALUES($1,'EXPENSE','NEEDS_REVIEW',25000,now(),'Warung') RETURNING id`, household).Scan(&transaction); err != nil {
		t.Fatal(err)
	}
	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(ctx)
	if err := reviewdomain.ReopenTransactionReview(ctx, first, household, transaction); err != nil {
		t.Fatal(err)
	}
	second, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(ctx)
	done := make(chan error, 1)
	go func() { done <- reviewdomain.ReopenTransactionReview(ctx, second, household, transaction) }()
	waitUntilBlockedBy(t, pool, first)
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("concurrent reversal failed instead of reusing the active item: %v", err)
	}
	if err := second.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE transaction_id=$1 AND status IN ('OPEN','PENDING_SEND')`, transaction).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active review items = %d, want 1", active)
	}
}
