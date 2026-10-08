-- +goose Up
-- CEU-02: bind a Telegram reply to the evidence it points at.
--
-- 1. source_event.telegram_chat_id lets "reply to my upload" resolve by household +
--    chat + message id. Message ids are only unique per chat, and each household
--    member has their own private chat, so the message id alone is ambiguous.
ALTER TABLE source_event ADD COLUMN IF NOT EXISTS telegram_chat_id BIGINT;
CREATE INDEX IF NOT EXISTS source_event_telegram_reply_idx
  ON source_event(household_id, telegram_chat_id, telegram_message_id)
  WHERE telegram_chat_id IS NOT NULL AND telegram_message_id IS NOT NULL;

-- Backfill from the stored raw update; rows without a readable chat id stay NULL
-- and are simply not reply-bindable.
UPDATE source_event se
SET telegram_chat_id = (sep.payload_json->'message'->'chat'->>'id')::bigint
FROM source_event_payload sep
WHERE sep.source_event_id = se.id
  AND se.source_type = 'TELEGRAM_IMAGE'
  AND se.telegram_chat_id IS NULL
  AND (sep.payload_json->'message'->'chat'->>'id') ~ '^-?[0-9]{1,18}$';

-- 2. The bot's own outbound evidence notices, so a reply to one binds to the
--    document it was about. Review cards keep binding through
--    review_request_recipient; this table is for plain notices only.
CREATE TABLE telegram_message_binding (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  household_id UUID NOT NULL REFERENCES household(id),
  telegram_chat_id BIGINT NOT NULL,
  telegram_message_id BIGINT NOT NULL,
  entity_type TEXT NOT NULL CHECK (entity_type IN ('DOCUMENT')),
  entity_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (telegram_chat_id, telegram_message_id)
);
CREATE INDEX telegram_message_binding_household_idx ON telegram_message_binding(household_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS telegram_message_binding_household_idx;
DROP TABLE IF EXISTS telegram_message_binding;
DROP INDEX IF EXISTS source_event_telegram_reply_idx;
ALTER TABLE source_event DROP COLUMN IF EXISTS telegram_chat_id;
