-- +goose Up
ALTER TABLE product_telemetry_event DROP CONSTRAINT product_telemetry_event_interaction_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_interaction_check
  CHECK (
    event_type <> 'INTERACTION'
    OR (
      action IN ('ANSWERED', 'UNMATCHED', 'AMBIGUOUS', 'CLOSED_CARD_REPLY', 'REPEATED_INPUT', 'STEP_SHOWN', 'FINISHED_ON_WEB', 'REPLY_REPAIRED')
      AND (source_type IS NULL OR source_type IN ('TAP', 'REPLY', 'THREAD_REPLY', 'PLAIN', 'LABEL_TEXT'))
      AND (source_type IS NULL OR action = 'ANSWERED')
      AND (decision_source IS NULL OR (action = 'REPLY_REPAIRED' AND decision_source IN ('EMPTY', 'BROKEN_TEXT', 'INTERNAL_CODE', 'TELEGRAM_MECHANICS')))
      AND (action <> 'REPLY_REPAIRED' OR decision_source IS NOT NULL)
      AND screen IS NULL
      AND changed_fields = '{}' AND bounded_choices = 0
    )
  );

-- +goose Down
DELETE FROM product_telemetry_event WHERE event_type='INTERACTION' AND action='REPLY_REPAIRED';
ALTER TABLE product_telemetry_event DROP CONSTRAINT product_telemetry_event_interaction_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_interaction_check
  CHECK (
    event_type <> 'INTERACTION'
    OR (
      action IN ('ANSWERED', 'UNMATCHED', 'AMBIGUOUS', 'CLOSED_CARD_REPLY', 'REPEATED_INPUT', 'STEP_SHOWN', 'FINISHED_ON_WEB')
      AND (source_type IS NULL OR source_type IN ('TAP', 'REPLY', 'THREAD_REPLY', 'PLAIN', 'LABEL_TEXT'))
      AND (source_type IS NULL OR action = 'ANSWERED')
      AND screen IS NULL
      AND changed_fields = '{}' AND bounded_choices = 0
    )
  );
