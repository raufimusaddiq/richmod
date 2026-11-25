-- +goose Up
UPDATE source_event se
SET telegram_chat_id = (sep.payload_json#>>'{message,chat,id}')::bigint,
    telegram_message_id = (sep.payload_json#>>'{message,message_id}')::bigint
FROM source_event_payload sep
WHERE sep.source_event_id = se.id
  AND se.source_type = 'TELEGRAM_TEXT'
  AND se.received_at > now() - interval '30 days'
  AND se.telegram_message_id IS NULL
  AND (sep.payload_json#>>'{message,chat,id}') ~ '^-?[0-9]{1,18}$'
  AND (sep.payload_json#>>'{message,message_id}') ~ '^[0-9]{1,18}$';

-- +goose Down
UPDATE source_event SET telegram_chat_id = NULL, telegram_message_id = NULL WHERE source_type = 'TELEGRAM_TEXT';
