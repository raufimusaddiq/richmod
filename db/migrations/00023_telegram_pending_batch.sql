-- +goose Up
CREATE TABLE telegram_pending_batch (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    telegram_user_id BIGINT NOT NULL,
    telegram_chat_id BIGINT NOT NULL,
    source_event_id UUID NOT NULL REFERENCES source_event(id),
    items_json JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING','CONFIRMED','CANCELLED','EXPIRED')),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '5 minutes',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX telegram_pending_batch_open ON telegram_pending_batch(telegram_user_id,telegram_chat_id) WHERE status='PENDING';
CREATE INDEX telegram_pending_batch_expiry ON telegram_pending_batch(expires_at) WHERE status='PENDING';
-- +goose Down
DROP TABLE telegram_pending_batch;
