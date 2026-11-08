package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A worker that ran past its lease and lost the job to a reclaim must not be
// able to finish, fail or defer the run that now owns it.
func TestStaleWorkerCannotSettleAReclaimedJob(t *testing.T) {
	q, pool := isolatedQueue(t)
	ctx := context.Background()
	insertTextJob(t, pool, "30000000-0000-4000-8000-000000000001", serialHouseholdA, 30*time.Second)
	first, found, err := q.Claim(ctx, "slow", "DEFAULT")
	if err != nil || !found {
		t.Fatalf("first claim = %v %v", found, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE job SET locked_at=now()-interval '10 minutes' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	second, found, err := q.Claim(ctx, "fast", "DEFAULT")
	if err != nil || !found || second.ID != first.ID || second.LockedBy != "fast" {
		t.Fatalf("reclaim = %+v %v %v", second, found, err)
	}
	if err := q.Succeed(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := q.FailWithHook(ctx, first, errors.New("late failure"), nil); err != nil {
		t.Fatal(err)
	}
	if err := q.Defer(ctx, first, errors.New("late defer")); err != nil {
		t.Fatal(err)
	}
	var status, lockedBy string
	if err := pool.QueryRow(ctx, `SELECT status,locked_by FROM job WHERE id=$1`, first.ID).Scan(&status, &lockedBy); err != nil {
		t.Fatal(err)
	}
	if status != "RUNNING" || lockedBy != "fast" {
		t.Fatalf("stale worker changed the job: status=%s locked_by=%s", status, lockedBy)
	}
	if err := q.Succeed(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM job WHERE id=$1`, first.ID).Scan(&status); err != nil || status != "SUCCEEDED" {
		t.Fatalf("owner could not finish: %s %v", status, err)
	}
}

// A job reclaimed after it already spent every attempt is reported exhausted
// instead of being run (and reclaimed) forever.
func TestReclaimPastMaxAttemptsIsExhausted(t *testing.T) {
	q, pool := isolatedQueue(t)
	ctx := context.Background()
	insertTextJob(t, pool, "30000000-0000-4000-8000-000000000002", serialHouseholdA, 30*time.Second)
	if _, err := pool.Exec(ctx, `UPDATE job SET status='RUNNING',attempts=max_attempts,locked_at=now()-interval '10 minutes',locked_by='dead' WHERE id='30000000-0000-4000-8000-000000000002'`); err != nil {
		t.Fatal(err)
	}
	job, found, err := q.Claim(ctx, "w1", "DEFAULT")
	if err != nil || !found {
		t.Fatalf("claim = %v %v", found, err)
	}
	if !Exhausted(job) {
		t.Fatalf("attempts=%d max=%d should be exhausted", job.Attempts, job.MaxAttempts)
	}
	err = LeaseExpiredError{Attempts: job.Attempts, MaxAttempts: job.MaxAttempts}
	if !Final(job, err) {
		t.Fatal("lease expiry must be a final failure")
	}
	if err := q.FailWithHook(ctx, job, err, nil); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM job WHERE id=$1`, job.ID).Scan(&status); err != nil || status != "FAILED" {
		t.Fatalf("status = %s %v, want FAILED", status, err)
	}
}
