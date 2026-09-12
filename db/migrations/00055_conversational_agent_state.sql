-- +goose Up
-- ADR-033 introduces conversational text and multi-read model phases while the
-- strict NativeToolCall contract remains available for extraction/classification.
-- Telegram correction state already supports nullable time/category/description
-- since migration 00040.
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL','AGENT_TEXT','AGENT_TOOLS'));

-- +goose Down
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL'));
