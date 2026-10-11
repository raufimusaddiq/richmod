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
}

type Queue struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Queue { return &Queue{pool: pool} }

func (q *Queue) Claim(ctx context.Context, workerID, lane string) (Job, bool, error) {
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, false, fmt.Errorf("begin job claim: %w", err)
	}
	defer tx.Rollback(ctx)

	var job Job
	err = tx.QueryRow(ctx, `
		SELECT id,type,payload_json,attempts+1,max_attempts
		FROM job
		WHERE lane=$1 AND ((status='PENDING' AND run_after<=now())
		   OR (status='RUNNING' AND locked_at<now()-interval '5 minutes'))
		ORDER BY run_after,created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, lane).Scan(&job.ID, &job.Type, &job.Payload, &job.Attempts, &job.MaxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("select job: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job SET status='RUNNING',attempts=$2,locked_at=now(),locked_by=$3,started_at=now(),finished_at=NULL,updated_at=now() WHERE id=$1`, job.ID, job.Attempts, workerID); err != nil {
		return Job{}, false, fmt.Errorf("lock job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("commit job claim: %w", err)
	}
	return job, true, nil
}

func (q *Queue) Succeed(ctx context.Context, jobID string) error {
	_, err := q.pool.Exec(ctx, `UPDATE job SET status='SUCCEEDED',locked_at=NULL,locked_by=NULL,last_error=NULL,finished_at=now(),updated_at=now() WHERE id=$1 AND status='RUNNING'`, jobID)
	return err
}

// Final reports whether a failed attempt is the job's last: it either used its
// attempts or failed with an error that must not be retried.
func Final(job Job, processErr error) bool {
	return job.Attempts >= job.MaxAttempts || isPermanent(processErr)
}

// FailWithHook records a failed attempt: the job is rescheduled, or marked FAILED
// when Final says it is the last. On the last attempt it also runs onFinal in the
// same transaction, so a caller that must tell someone the job is giving up
// (queue a reply) does it atomically: either the job is marked FAILED and the
// notice is queued, or neither happens and the stale-lock claim retries the job.
// Only the worker that still owns the job runs onFinal. If onFinal itself cannot
// succeed (bad payload, say), the job is still marked FAILED without it, because
// a job that can never be failed would be reclaimed forever.
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
	delaySeconds := int(time.Duration(1<<min(job.Attempts, 8)) * time.Second / time.Second)
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE job SET status=$2,run_after=now()+$3*interval '1 second',locked_at=NULL,locked_by=NULL,last_error=$4,finished_at=CASE WHEN $2='FAILED' THEN now() ELSE NULL END,updated_at=now() WHERE id=$1 AND status='RUNNING'`, job.ID, status, delaySeconds, truncate(processErr.Error(), 1000))
	if err != nil {
		return err
	}
	// Only the worker that still owns the job may run the final step.
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

func isPermanent(err error) bool {
	var permanent interface{ Permanent() bool }
	return errors.As(err, &permanent) && permanent.Permanent()
}

func classifyLane(jobType string) string {
	switch jobType {
	case "PROCESS_TELEGRAM_CALLBACK", "SEND_TELEGRAM_MESSAGE", "EDIT_TELEGRAM_MESSAGE", "RETIRE_TELEGRAM_REVIEW_CARD", "COMPLETE_BANK_REVIEW":
		return "INTERACTIVE"
	case "PROCESS_TELEGRAM_TEXT":
		return "CHAT"
	case "FETCH_TELEGRAM_IMAGE", "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT", "GENERATE_INSIGHT", "GENERATE_CYCLE_RESIDUAL_REVIEW", "PROCESS_BANK_EMAIL", "PROCESS_FINANCIAL_EMAIL", "PROCESS_FINANCIAL_EMAIL_PREVIEW":
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
	// Errors from ctx-bound processors carry the budget timeout; the value is
	// not always exposed, so the executor logs it via slog. We persist only the
	// error class here and rely on the queue's last_error for the message.
	return 0
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
