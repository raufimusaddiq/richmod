package reviewdomain

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSweepFailedSourcesAddsOneItemPerStuckSource(t *testing.T) {
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
	var household string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Sweep %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	source := func(name, status, jobStatus string) string {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,$4) RETURNING id`, household, fmt.Sprintf("%s-%d", name, stamp), []byte(name), status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if jobStatus != "" {
			if _, err := pool.Exec(ctx, `INSERT INTO job(type,payload_json,status) VALUES('PROCESS_DOCUMENT',jsonb_build_object('source_event_id',$1::text),$2)`, id, jobStatus); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	stuck := source("stuck", "RECEIVED", "FAILED")
	failed := source("failed", "FAILED", "")
	retrying := source("retrying", "RECEIVED", "PENDING")
	fresh := source("fresh", "RECEIVED", "")
	done := source("done", "PROCESSED", "FAILED")
	count := func(id string) (n int) {
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_action WHERE household_id=$1 AND action_type=$2 AND dedupe_key=$3`, household, FailedSourceActionType, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for run := 0; run < 2; run++ { // second run proves idempotence
		if _, err := SweepFailedSources(ctx, pool, 1000); err != nil {
			t.Fatal(err)
		}
		for id, want := range map[string]int{stuck: 1, failed: 1, retrying: 0, fresh: 0, done: 0} {
			if got := count(id); got != want {
				t.Fatalf("run %d source %s: items=%d want %d", run, id, got, want)
			}
		}
	}
}
