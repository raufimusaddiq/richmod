-- Lightweight, privacy-safe screen/action measurement. UI events reuse the
-- bounded product event table: `screen` and `action` are allow-listed names,
-- never a financial value, amount, description, merchant, prompt or free text,
-- and a UI event carries no source/transaction/review reference.
-- +goose Up
ALTER TABLE product_telemetry_event ADD COLUMN screen TEXT;

ALTER TABLE product_telemetry_event DROP CONSTRAINT IF EXISTS product_telemetry_event_event_type_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_event_type_check
  CHECK (event_type IN ('REVIEW_TURN', 'AUTO_CONFIRM_CORRECTION', 'CEU_BINDING', 'UI_EVENT'));

ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_ui_event_check
  CHECK (
    (event_type = 'UI_EVENT'
      AND screen IN ('home','transactions','analytics','wealth','inbox','documents','household','settings','admin')
      AND action IN ('SCREEN_VIEW','NAV_CLICK')
      AND source_event_id IS NULL AND transaction_id IS NULL AND review_item_id IS NULL
      AND source_type IS NULL AND decision_policy_version IS NULL AND decision_source IS NULL
      AND changed_fields = '{}' AND bounded_choices = 0)
    OR (event_type <> 'UI_EVENT' AND screen IS NULL)
  );

-- +goose Down
DELETE FROM product_telemetry_event WHERE event_type='UI_EVENT';
ALTER TABLE product_telemetry_event DROP CONSTRAINT IF EXISTS product_telemetry_event_ui_event_check;
ALTER TABLE product_telemetry_event DROP CONSTRAINT IF EXISTS product_telemetry_event_event_type_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_event_type_check
  CHECK (event_type IN ('REVIEW_TURN', 'AUTO_CONFIRM_CORRECTION', 'CEU_BINDING'));
ALTER TABLE product_telemetry_event DROP COLUMN screen;
