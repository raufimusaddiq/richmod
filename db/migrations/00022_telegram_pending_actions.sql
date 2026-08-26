-- +goose Up
CREATE TABLE telegram_pending_action (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    telegram_user_id BIGINT NOT NULL,
    telegram_chat_id BIGINT NOT NULL,
    transaction_id UUID NOT NULL REFERENCES transaction(id),
    proposed_transaction_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING','CONFIRMED','CANCELLED','EXPIRED')),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '5 minutes',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX telegram_pending_action_open ON telegram_pending_action(telegram_user_id,telegram_chat_id) WHERE status='PENDING';
CREATE INDEX telegram_pending_action_expiry ON telegram_pending_action(expires_at) WHERE status='PENDING';
-- +goose Down
DROP TABLE telegram_pending_action;
