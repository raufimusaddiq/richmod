-- +goose Up
ALTER TABLE source_event ADD COLUMN IF NOT EXISTS telegram_chat_id BIGINT;
CREATE INDEX IF NOT EXISTS source_event_telegram_reply_idx
  ON source_event(household_id, telegram_chat_id, telegram_message_id)
  WHERE telegram_chat_id IS NOT NULL AND telegram_message_id IS NOT NULL;

UPDATE source_event se
SET telegram_chat_id = (sep.payload_json->'message'->'chat'->>'id')::bigint
FROM source_event_payload sep
WHERE sep.source_event_id = se.id
  AND se.source_type = 'TELEGRAM_IMAGE'
  AND se.telegram_chat_id IS NULL
  AND (sep.payload_json->'message'->'chat'->>'id') ~ '^-?[0-9]{1,18}$';

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
