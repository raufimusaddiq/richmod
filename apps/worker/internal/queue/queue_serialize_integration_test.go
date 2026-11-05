package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// isolatedQueue gives a test its own job table in a private schema that every
// pooled connection resolves first, so concurrent claims run against real
// cross-session rows without touching other tests' jobs.
func isolatedQueue(t *testing.T) (*Queue, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("claim_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE TABLE `+schema+`.job (LIKE public.job INCLUDING DEFAULTS)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	config.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return New(pool), pool
}

const (
	serialHouseholdA = "00000000-0000-4000-8000-0000000000a1"
	serialHouseholdB = "00000000-0000-4000-8000-0000000000b1"
)

func insertTextJob(t *testing.T, pool *pgxpool.Pool, id, household string, age time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO job(id,type,lane,household_id,status,payload_json,run_after,created_at)
		VALUES($1,'PROCESS_TELEGRAM_TEXT','DEFAULT',$2,'PENDING','{}',now()-$3*interval '1 second',now()-$3*interval '1 second')`, id, household, age.Seconds()); err != nil {
		t.Fatal(err)
	}
}

// Two DEFAULT loops must never run two Telegram messages of one household at
// once, and must still serve another household in parallel.
func TestClaimSerializesTelegramTextPerHouseholdUnderConcurrency(t *testing.T) {
	q, pool := isolatedQueue(t)
	ctx := context.Background()
	insertTextJob(t, pool, "20000000-0000-4000-8000-000000000001", serialHouseholdA, 30*time.Second)
	insertTextJob(t, pool, "20000000-0000-4000-8000-000000000002", serialHouseholdA, 20*time.Second)
	insertTextJob(t, pool, "20000000-0000-4000-8000-000000000003", serialHouseholdA, 10*time.Second)
	insertTextJob(t, pool, "20000000-0000-4000-8000-000000000004", serialHouseholdB, 5*time.Second)

	var mu sync.Mutex
	claimed := map[string]string{}
	var wg sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			job, found, err := q.Claim(ctx, fmt.Sprintf("w%d", worker), "DEFAULT")
			if err != nil {
				t.Error(err)
				return
			}
			if found {
				mu.Lock()
				claimed[job.ID] = job.HouseholdID
				mu.Unlock()
			}
		}(worker)
	}
	wg.Wait()
	if len(claimed) != 2 || claimed["20000000-0000-4000-8000-000000000001"] != serialHouseholdA || claimed["20000000-0000-4000-8000-000000000004"] != serialHouseholdB {
		t.Fatalf("concurrent claims = %v, want A's oldest and B's only", claimed)
	}
	// While A's first message runs, its next one stays queued.
	if job, found, err := q.Claim(ctx, "w9", "DEFAULT"); err != nil || found {
		t.Fatalf("claimed %s while household A's message was running (err=%v)", job.ID, err)
	}
	if err := q.Succeed(ctx, "20000000-0000-4000-8000-000000000001"); err != nil {
		t.Fatal(err)
	}
	job, found, err := q.Claim(ctx, "w9", "DEFAULT")
	if err != nil || !found || job.ID != "20000000-0000-4000-8000-000000000002" {
		t.Fatalf("after A1 finished, claim = %v %v %v, want A2", job.ID, found, err)
	}
	// A2 fails and is rescheduled; A3 must not overtake it while the retry waits.
	if err := q.FailWithHook(ctx, job, errors.New("transient"), nil); err != nil {
		t.Fatal(err)
	}
	if job, found, err := q.Claim(ctx, "w9", "DEFAULT"); err != nil || found {
		t.Fatalf("household A's next message overtook its retry: claimed %s (err=%v)", job.ID, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE job SET run_after=now()-interval '1 second' WHERE id='20000000-0000-4000-8000-000000000002'`); err != nil {
		t.Fatal(err)
	}
	job, found, err = q.Claim(ctx, "w9", "DEFAULT")
	if err != nil || !found || job.ID != "20000000-0000-4000-8000-000000000002" {
		t.Fatalf("due retry claim = %v %v %v, want A2 before A3", job.ID, found, err)
	}
}

// A stale lease (a crashed worker) does not block the household forever: the
// stale job itself is reclaimed, still before the household's newer message.
func TestClaimReclaimsStaleTelegramTextBeforeNewerMessage(t *testing.T) {
	q, pool := isolatedQueue(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO job(id,type,lane,household_id,status,payload_json,run_after,created_at,locked_at,started_at,attempts)
		VALUES('30000000-0000-4000-8000-000000000001','PROCESS_TELEGRAM_TEXT','DEFAULT',$1,'RUNNING','{}',now()-interval '20 minutes',now()-interval '20 minutes',now()-interval '10 minutes',now()-interval '10 minutes',1)`, serialHouseholdA); err != nil {
		t.Fatal(err)
	}
	insertTextJob(t, pool, "30000000-0000-4000-8000-000000000002", serialHouseholdA, time.Second)
	job, found, err := q.Claim(ctx, "w1", "DEFAULT")
	if err != nil || !found || job.ID != "30000000-0000-4000-8000-000000000001" {
		t.Fatalf("claim = %v %v %v, want the stale older message", job.ID, found, err)
	}
	if job, found, _ := q.Claim(ctx, "w2", "DEFAULT"); found {
		t.Fatalf("newer message %s ran beside the reclaimed one", job.ID)
	}
}

// A deferral (AI gateway down) gives the attempt back, backs off from five
// minutes, and does not block a household's newer, never-deferred message
// (so deterministic replies still flow); once that one is deferred too, it
// queues behind the older one again.
func TestDeferGivesAttemptsBackAndKeepsDeterministicRepliesFlowing(t *testing.T) {
	q, pool := isolatedQueue(t)
	ctx := context.Background()
	insertTextJob(t, pool, "40000000-0000-4000-8000-000000000001", serialHouseholdA, 30*time.Second)
	job, found, err := q.Claim(ctx, "w1", "DEFAULT")
	if err != nil || !found {
		t.Fatalf("claim: %v %v", found, err)
	}
	if err := q.Defer(ctx, job, errors.New("gateway unavailable")); err != nil {
		t.Fatal(err)
	}
	var attempts, deferrals int
	var wait float64
	if err := pool.QueryRow(ctx, `SELECT attempts,deferrals,extract(epoch FROM run_after-now()) FROM job WHERE id=$1`, job.ID).Scan(&attempts, &deferrals, &wait); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || deferrals != 1 || wait < 290 || wait > 310 {
		t.Fatalf("deferred job attempts=%d deferrals=%d wait=%.0fs", attempts, deferrals, wait)
	}
	if DeferralDelay(1) != 10*time.Minute || DeferralDelay(10) != time.Hour {
		t.Fatalf("backoff %s / %s", DeferralDelay(1), DeferralDelay(10))
	}
	insertTextJob(t, pool, "40000000-0000-4000-8000-000000000002", serialHouseholdA, time.Second)
	next, found, err := q.Claim(ctx, "w1", "DEFAULT")
	if err != nil || !found || next.ID != "40000000-0000-4000-8000-000000000002" {
		t.Fatalf("a fresh message must not wait behind a deferred one: %v %v %v", next.ID, found, err)
	}
	if err := q.Defer(ctx, next, errors.New("gateway unavailable")); err != nil {
		t.Fatal(err)
	}
	// Both are due again: the older one goes first and the newer waits for it.
	if _, err := pool.Exec(ctx, `UPDATE job SET run_after=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	first, found, err := q.Claim(ctx, "w1", "DEFAULT")
	if err != nil || !found || first.ID != job.ID {
		t.Fatalf("after the outage the older message must run first: %v %v %v", first.ID, found, err)
	}
	if extra, found, _ := q.Claim(ctx, "w2", "DEFAULT"); found {
		t.Fatalf("the newer message %s ran beside the older one", extra.ID)
	}
}
