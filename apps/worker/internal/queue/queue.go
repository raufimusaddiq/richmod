package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (q *Queue) Fail(ctx context.Context, job Job, processErr error) error {
	status := "PENDING"
	if job.Attempts >= job.MaxAttempts {
		status = "FAILED"
	}
	delaySeconds := int(time.Duration(1<<min(job.Attempts, 8)) * time.Second / time.Second)
	_, err := q.pool.Exec(ctx, `UPDATE job SET status=$2,run_after=now()+$3*interval '1 second',locked_at=NULL,locked_by=NULL,last_error=$4,finished_at=CASE WHEN $2='FAILED' THEN now() ELSE NULL END,updated_at=now() WHERE id=$1 AND status='RUNNING'`, job.ID, status, delaySeconds, truncate(processErr.Error(), 1000))
	if err != nil {
		return err
	}
	if _, logErr := q.pool.Exec(ctx, `INSERT INTO job_retry_log(job_id,attempt,lane,job_type,error_class,duration_ms) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (job_id,attempt) DO NOTHING`, job.ID, job.Attempts, classifyLane(job.Type), job.Type, classifyError(processErr), durationMillis(processErr)); logErr != nil {
		return fmt.Errorf("write job retry log: %w", logErr)
	}
	return nil
}

func classifyLane(jobType string) string {
	switch jobType {
	case "PROCESS_TELEGRAM_CALLBACK", "SEND_TELEGRAM_MESSAGE", "EDIT_TELEGRAM_MESSAGE", "COMPLETE_BANK_REVIEW":
		return "INTERACTIVE"
	case "PROCESS_TELEGRAM_TEXT", "PROCESS_TELEGRAM_REVIEW_TEXT":
		return "CHAT"
	case "FETCH_TELEGRAM_IMAGE", "FINALIZE_TELEGRAM_MEDIA_GROUP", "PROCESS_DOCUMENT", "PROCESS_PAYSLIP", "PROCESS_RECEIPT", "PROCESS_TRANSACTION_SCREENSHOT", "GENERATE_INSIGHT", "PROCESS_BANK_EMAIL":
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
	case strings.Contains(msg, "context deadline exceeded"):
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
