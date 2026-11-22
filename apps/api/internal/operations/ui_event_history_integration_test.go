package operations

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOldUIEventsDoNotClearPreMigrationTelemetryHistory(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var household string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("UI history %d", uniqueStamp())).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,screen,action,occurred_at) VALUES($1,'UI_EVENT','home','SCREEN_VIEW',now()-interval '40 days')`, household); err != nil {
		t.Fatal(err)
	}
	covered := func() bool {
		aggregate, err := aggregateHandler(pool).loadProductAggregate(ctx, household)
		if err != nil {
			t.Fatal(err)
		}
		return !slices.Contains(aggregate.Coverage, "pre_migration_telemetry_history")
	}
	if covered() {
		t.Fatal("40-day-old UI_EVENT rows cleared pre_migration_telemetry_history")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,action,occurred_at) VALUES($1,'REVIEW_TURN','CONFIRM_REVIEW',now()-interval '40 days')`, household); err != nil {
		t.Fatal(err)
	}
	if !covered() {
		t.Fatal("a 40-day-old product event should clear the gap")
	}
}
