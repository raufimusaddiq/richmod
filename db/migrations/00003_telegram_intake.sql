-- +goose Up
CREATE TABLE telegram_identity (
    telegram_user_id BIGINT PRIMARY KEY,
    household_id UUID NOT NULL REFERENCES household(id),
    user_id UUID NOT NULL REFERENCES "user"(id),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id, user_id)
);

CREATE TABLE source_event_payload (
    source_event_id UUID PRIMARY KEY REFERENCES source_event(id),
    payload_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE job (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type TEXT NOT NULL,
    payload_json JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    run_after TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX job_claim_idx ON job (run_after, created_at) WHERE status = 'PENDING';

-- +goose Down
DROP TABLE job;
DROP TABLE source_event_payload;
DROP TABLE telegram_identity;
