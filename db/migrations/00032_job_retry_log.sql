-- +goose Up
-- Record every job retry attempt so retry history survives container
-- restarts and Docker log rotation. Each row captures the error class
-- (no payload/message content) plus lifecycle timing.
CREATE TABLE IF NOT EXISTS job_retry_log (
  id           BIGSERIAL PRIMARY KEY,
  job_id       UUID NOT NULL,
  attempt      INTEGER NOT NULL,
  lane         TEXT NOT NULL,
  job_type     TEXT NOT NULL,
  error_class  TEXT NOT NULL,
  failed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  retried_at   TIMESTAMPTZ,
  duration_ms  INTEGER,
  CONSTRAINT job_retry_log_unique_attempt UNIQUE (job_id, attempt)
);

CREATE INDEX IF NOT EXISTS idx_job_retry_log_failed_at
  ON job_retry_log (failed_at DESC);

CREATE INDEX IF NOT EXISTS idx_job_retry_log_job_type_failed_at
  ON job_retry_log (job_type, failed_at DESC);

-- +goose Down
DROP TABLE IF EXISTS job_retry_log;
