package uitelemetry

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestUIEventsAreHouseholdScopedAndConstrainedByTheDatabase(t *testing.T) {
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
	stamp := testsupport.UniqueStamp()
	var a, b string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("UI telemetry A %d", stamp)).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("UI telemetry B %d", stamp)).Scan(&b); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(pool)
	if err := h.record(ctx, a, "analytics", "SCREEN_VIEW"); err != nil {
		t.Fatal(err)
	}
	var countA, countB int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_telemetry_event WHERE household_id=$1 AND event_type='UI_EVENT' AND screen='analytics' AND action='SCREEN_VIEW'`, a).Scan(&countA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM product_telemetry_event WHERE household_id=$1`, b).Scan(&countB); err != nil {
		t.Fatal(err)
	}
	if countA != 1 || countB != 0 {
		t.Fatalf("household scoping: a=%d b=%d", countA, countB)
	}
	for _, bad := range [][2]string{{"Rp 9.923.834", "SCREEN_VIEW"}, {"home", "free text"}} {
		if err := h.record(ctx, a, bad[0], bad[1]); err == nil {
			t.Fatalf("database accepted %v", bad)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,action) VALUES($1,'UI_EVENT','SCREEN_VIEW')`, a); err == nil {
		t.Fatal("a UI_EVENT needs a screen")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,event_type,action,screen) VALUES($1,'REVIEW_TURN','IGNORE','home')`, a); err == nil {
		t.Fatal("screen is only valid on UI_EVENT rows")
	}
}
