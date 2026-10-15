-- +goose Up
ALTER TABLE telegram_pending_action ADD COLUMN confirmation_token UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE telegram_pending_batch ADD COLUMN confirmation_token UUID NOT NULL DEFAULT gen_random_uuid();

-- +goose Down
ALTER TABLE telegram_pending_batch DROP COLUMN confirmation_token;
ALTER TABLE telegram_pending_action DROP COLUMN confirmation_token;
