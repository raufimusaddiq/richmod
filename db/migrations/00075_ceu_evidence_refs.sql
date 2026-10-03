-- +goose Up
-- CEU-01: bounded, model-safe evidence references. An evidence ref is an opaque,
-- expiring, household + Telegram user + chat scoped lookup key whose entity_id is
-- a document id that never leaves the server. No new table: the existing
-- reference table already carries scope and expiry.
ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_entity_type_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_entity_type_check
  CHECK (entity_type IN ('TRANSACTION','REVIEW','EVIDENCE'));

ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_ref_key_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_ref_key_check
  CHECK (
    ref_key ~ '^(tx|review)_[0-9]+$'
    OR ref_key ~ '^p[0-9]+r[0-9]+_tx[0-9]+$'
    OR ref_key ~ '^a[0-9a-f]{8}_p[0-9]+r[0-9]+_tx[0-9]+$'
    OR ref_key ~ '^a[0-9a-f]{8}_p[0-9]+r[0-9]+_ev[0-9]+$'
  );

-- Binding-outcome telemetry reuses the bounded product event table: the action
-- is one allow-listed outcome name, never text, a value, or an identifier.
ALTER TABLE product_telemetry_event DROP CONSTRAINT IF EXISTS product_telemetry_event_event_type_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_event_type_check
  CHECK (event_type IN ('REVIEW_TURN', 'AUTO_CONFIRM_CORRECTION', 'CEU_BINDING'));

-- +goose Down
DELETE FROM product_telemetry_event WHERE event_type='CEU_BINDING';
ALTER TABLE product_telemetry_event DROP CONSTRAINT IF EXISTS product_telemetry_event_event_type_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_event_type_check
  CHECK (event_type IN ('REVIEW_TURN', 'AUTO_CONFIRM_CORRECTION'));

DELETE FROM telegram_turn_reference WHERE entity_type='EVIDENCE';
ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_ref_key_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_ref_key_check
  CHECK (
    ref_key ~ '^(tx|review)_[0-9]+$'
    OR ref_key ~ '^p[0-9]+r[0-9]+_tx[0-9]+$'
    OR ref_key ~ '^a[0-9a-f]{8}_p[0-9]+r[0-9]+_tx[0-9]+$'
  );
ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_entity_type_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_entity_type_check
  CHECK (entity_type IN ('TRANSACTION','REVIEW'));
