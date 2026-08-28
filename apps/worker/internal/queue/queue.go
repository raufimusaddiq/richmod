package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	return err
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
