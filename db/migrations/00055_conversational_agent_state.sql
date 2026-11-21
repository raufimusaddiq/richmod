-- +goose Up
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL','AGENT_TEXT','AGENT_TOOLS'));

ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_ref_key_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_ref_key_check
  CHECK (
    ref_key ~ '^(tx|review)_[0-9]+$'
    OR ref_key ~ '^p[0-9]+r[0-9]+_tx[0-9]+$'
    OR ref_key ~ '^a[0-9a-f]{8}_p[0-9]+r[0-9]+_tx[0-9]+$'
  );

-- +goose Down
ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_ref_key_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_ref_key_check
  CHECK (ref_key ~ '^(tx|review)_[0-9]+$');

ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL'));
