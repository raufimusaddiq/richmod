package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Job struct {
	ID          string
	Type        string
	Payload     json.RawMessage
	Attempts    int
	MaxAttempts int
	HouseholdID string
	Deferrals   int
	LockedBy    string
}

type Queue struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Queue { return &Queue{pool: pool} }

func (q *Queue) Claim(ctx context.Context, workerID, lane string) (Job, bool, error) {
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, false, fmt.Errorf("begin job claim: %w", err)
	}
	defer tx.Rollback(ctx)
	job, found, err := claimIn(ctx, tx, workerID, lane)
	if err != nil || !found {
		return Job{}, found, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("commit job claim: %w", err)
	}
	return job, true, nil
}

func claimIn(ctx context.Context, tx pgx.Tx, workerID, lane string) (Job, bool, error) {
	var job Job
	err := tx.QueryRow(ctx, claimSQL, lane).Scan(&job.ID, &job.Type, &job.Payload, &job.Attempts, &job.MaxAttempts, &job.HouseholdID, &job.Deferrals)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("select job: %w", err)
	}
	job.LockedBy = workerID
	if _, err := tx.Exec(ctx, `UPDATE job SET status='RUNNING',attempts=$2,locked_at=now(),locked_by=$3,started_at=now(),finished_at=NULL,updated_at=now() WHERE id=$1`, job.ID, job.Attempts, workerID); err != nil {
		return Job{}, false, fmt.Errorf("lock job: %w", err)
	}
	return job, true, nil
}

const claimSQL = `
	WITH heads AS (
		SELECT DISTINCT ON (household_id) id,household_id,run_after,created_at
		FROM job j
		WHERE j.lane=$1 AND ((j.status='PENDING' AND j.run_after<=now())
		   OR (j.status='RUNNING' AND j.locked_at<now()-interval '5 minutes'))
		  AND NOT (j.type IN (` + serializedChatTypesSQL + `) AND j.household_id IS NOT NULL AND EXISTS (
			SELECT 1 FROM job o
			WHERE o.lane=$1 AND o.household_id=j.household_id AND o.id<>j.id
			  AND o.type IN (` + serializedChatTypesSQL + `)
			  AND ((o.status='RUNNING' AND o.locked_at>=now()-interval '5 minutes')
			    OR (o.status IN ('PENDING','RUNNING') AND (o.created_at,o.id)<(j.created_at,j.id)
			        AND (o.status='RUNNING' OR o.deferrals=0 OR o.run_after<=now() OR j.deferrals>0)))))
		ORDER BY household_id,run_after,created_at
	), ranked AS (
		SELECT h.id,h.run_after,h.created_at,
		       CASE WHEN h.household_id IS NULL
		            THEN (SELECT max(p.started_at) FROM job p WHERE p.lane=$1 AND p.household_id IS NULL AND p.started_at IS NOT NULL)
		            ELSE (SELECT max(p.started_at) FROM job p WHERE p.lane=$1 AND p.household_id=h.household_id AND p.started_at IS NOT NULL)
		       END AS last_started
		FROM heads h
	)
	SELECT j.id,j.type,j.payload_json,j.attempts+1,j.max_attempts,COALESCE(j.household_id::text,''),j.deferrals
	FROM ranked r JOIN job j ON j.id=r.id
	ORDER BY r.last_started NULLS FIRST,r.run_after,r.created_at
	FOR UPDATE OF j SKIP LOCKED
	LIMIT 1`

const serializedChatTypesSQL = `'PROCESS_TELEGRAM_TEXT'`

const (
	deferralBase = 5 * time.Minute
	deferralCap  = time.Hour
)

func DeferralDelay(deferrals int) time.Duration {
	delay := deferralBase
	for i := 0; i < deferrals && delay < deferralCap; i++ {
		delay *= 2
	}
	if delay > deferralCap {
		delay = deferralCap
	}
	return delay
}

func (q *Queue) Defer(ctx context.Context, job Job, reason error) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := q.pool.Exec(ctx, `UPDATE job SET status='PENDING',attempts=GREATEST(attempts-1,0),deferrals=deferrals+1,
		run_after=now()+$2*interval '1 second',locked_at=NULL,locked_by=NULL,last_error=$3,updated_at=now()
		WHERE id=$1 AND status='RUNNING' AND locked_by=$4`, job.ID, DeferralDelay(job.Deferrals).Seconds(), truncate("DEFERRED: "+reason.Error(), 1000), job.LockedBy)
	return err
}

func (q *Queue) Succeed(ctx context.Context, job Job) error {
	_, err := q.pool.Exec(ctx, `UPDATE job SET status='SUCCEEDED',locked_at=NULL,locked_by=NULL,last_error=NULL,finished_at=now(),updated_at=now() WHERE id=$1 AND status='RUNNING' AND locked_by=$2`, job.ID, job.LockedBy)
	return err
}

const leaseRenewal = time.Minute

func (q *Queue) KeepLease(ctx context.Context, job Job) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(leaseRenewal)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				renew, cancel := context.WithTimeout(ctx, 10*time.Second)
				_, err := q.pool.Exec(renew, `UPDATE job SET locked_at=now() WHERE id=$1 AND status='RUNNING' AND locked_by=$2`, job.ID, job.LockedBy)
				cancel()
				if err != nil {
					slog.Default().Warn("job lease renewal failed", "job_id", job.ID, "error", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

type LeaseExpiredError struct{ Attempts, MaxAttempts int }

func (e LeaseExpiredError) Error() string {
	return fmt.Sprintf("job lease expired after %d of %d attempts", e.Attempts-1, e.MaxAttempts)
}
func (LeaseExpiredError) Permanent() bool { return true }

func Exhausted(job Job) bool { return job.Attempts > job.MaxAttempts }

func Final(job Job, processErr error) bool {
	return job.Attempts >= job.MaxAttempts || isPermanent(processErr)
}

func (q *Queue) FailWithHook(ctx context.Context, job Job, processErr error, onFinal func(context.Context, pgx.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if onFinal != nil && Final(job, processErr) {
		err := q.failTx(ctx, job, processErr, onFinal)
		if err == nil {
			return q.logRetry(ctx, job, processErr)
		}
		slog.Default().Error("final job failure step failed; failing the job without it", "job_id", job.ID, "type", job.Type, "error", err)
	}
	if err := q.failTx(ctx, job, processErr, nil); err != nil {
		return err
	}
	return q.logRetry(ctx, job, processErr)
}

func (q *Queue) failTx(ctx context.Context, job Job, processErr error, onFinal func(context.Context, pgx.Tx) error) error {
	status := "PENDING"
	if Final(job, processErr) {
		status = "FAILED"
	}
	delaySeconds := retryDelaySeconds(job, processErr)
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE job SET status=$2,run_after=now()+$3*interval '1 second',locked_at=NULL,locked_by=NULL,last_error=$4,finished_at=CASE WHEN $2='FAILED' THEN now() ELSE NULL END,updated_at=now() WHERE id=$1 AND status='RUNNING' AND locked_by=$5`, job.ID, status, delaySeconds, truncate(processErr.Error(), 1000), job.LockedBy)
	if err != nil {
		return err
	}
	if onFinal != nil && status == "FAILED" && tag.RowsAffected() == 1 {
		if err := onFinal(ctx, tx); err != nil {
			return fmt.Errorf("final failure step: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (q *Queue) logRetry(ctx context.Context, job Job, processErr error) error {
	if _, logErr := q.pool.Exec(ctx, `INSERT INTO job_retry_log(job_id,attempt,lane,job_type,error_class,duration_ms) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (job_id,attempt) DO NOTHING`, job.ID, job.Attempts, classifyLane(job.Type), job.Type, classifyError(processErr), durationMillis(processErr)); logErr != nil {
		return fmt.Errorf("write job retry log: %w", logErr)
	}
	return nil
}

func retryDelaySeconds(job Job, processErr error) int {
	delay := time.Duration(1<<min(job.Attempts, 8)) * time.Second
	var hinted interface{ RetryAfter() time.Duration }
	if errors.As(processErr, &hinted) {
		if wait := hinted.RetryAfter(); wait > 0 {
			delay = min(wait, time.Hour)
		}
	}
	return int((delay + time.Second - 1) / time.Second)
}

func isPermanent(err error) bool {
	var permanent interface{ Permanent() bool }
	return errors.As(err, &permanent) && permanent.Permanent()
}

func classifyLane(jobType string) string {
	switch jobType {
	case "PROCESS_TELEGRAM_CALLBACK", "SEND_TELEGRAM_MESSAGE", "EDIT_TELEGRAM_MESSAGE", "PROCESS_TELEGRAM_REVIEW_TEXT", "RETIRE_TELEGRAM_REVIEW_CARD", "COMPLETE_BANK_REVIEW":
		return "INTERACTIVE"
	case "FETCH_TELEGRAM_IMAGE", "FINALIZE_TELEGRAM_MEDIA_GROUP", "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT", "GENERATE_INSIGHT", "GENERATE_CYCLE_RESIDUAL_REVIEW", "PROCESS_BANK_EMAIL", "PROCESS_FINANCIAL_EMAIL", "PROCESS_FINANCIAL_EMAIL_PREVIEW":
		return "BACKGROUND"
	default:
		return "DEFAULT"
	}
}

func classifyError(err error) string {
	if err == nil {
		return "UNKNOWN"
	}
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "context deadline exceeded"):
		return "TIMEOUT"
	case strings.Contains(msg, "SQLSTATE"):
		return "DATABASE"
	case strings.Contains(msg, "Telegram"):
		return "TELEGRAM_API"
	case strings.Contains(msg, "LLM") || strings.Contains(msg, "gateway") || strings.Contains(msg, "Structured"):
		return "LLM_GATEWAY"
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host"):
		return "NETWORK"
	default:
		return "OTHER"
	}
}

func durationMillis(err error) int {
	if err == nil {
		return 0
	}
	return 0
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
