-- +goose Up
-- ADR-033 introduces conversational text and multi-read model phases while the
-- strict NativeToolCall contract remains available for extraction/classification.
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL','AGENT_TEXT','AGENT_TOOLS'));

-- Parallel READ tools need refs that are unique inside a turn. Keep the legacy
-- tx_N / review_N shape valid while allowing phase/read-scoped transaction refs
-- such as p1r2_tx1. Canonical UUIDs remain server-private.
ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_ref_key_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_ref_key_check
  CHECK (ref_key ~ '^(tx|review)_[0-9]+$' OR ref_key ~ '^p[0-9]+r[0-9]+_tx[0-9]+$');

-- +goose Down
ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_ref_key_check;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_ref_key_check
  CHECK (ref_key ~ '^(tx|review)_[0-9]+$');

ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL'));
