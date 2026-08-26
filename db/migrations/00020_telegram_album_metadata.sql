-- +goose Up
ALTER TABLE source_event ADD COLUMN IF NOT EXISTS telegram_media_group_id TEXT;
ALTER TABLE source_event ADD COLUMN IF NOT EXISTS telegram_message_id BIGINT;
CREATE INDEX IF NOT EXISTS source_event_telegram_album_idx ON source_event(household_id, telegram_media_group_id, telegram_message_id) WHERE telegram_media_group_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS source_event_telegram_album_idx;
ALTER TABLE source_event DROP COLUMN IF EXISTS telegram_message_id;
ALTER TABLE source_event DROP COLUMN IF EXISTS telegram_media_group_id;
