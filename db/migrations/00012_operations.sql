-- +goose Up
CREATE TABLE worker_heartbeat (
    worker_id TEXT PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX worker_heartbeat_last_seen_idx ON worker_heartbeat (last_seen_at DESC);

-- +goose Down
DROP TABLE worker_heartbeat;
