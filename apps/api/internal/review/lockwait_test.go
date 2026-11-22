package review

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func waitUntilBlockedBy(t *testing.T, pool *pgxpool.Pool, blocker pgx.Tx) {
	t.Helper()
	ctx := context.Background()
	var blockerPID int
	if err := blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no session ever blocked on the holder's lock")
}
