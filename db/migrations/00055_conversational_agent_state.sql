-- +goose Up
-- Sprint 1 conversational corrections may change date, category, or description.
-- Keep the pending action server-owned; the model only proposes typed fields.
ALTER TABLE telegram_pending_action
  ALTER COLUMN proposed_transaction_at DROP NOT NULL,
  ADD COLUMN proposed_category_id UUID REFERENCES category(id),
  ADD COLUMN proposed_description TEXT;

-- ADR-033 introduces conversational text and multi-read model phases while the
-- strict NativeToolCall contract remains available for extraction/classification.
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL','AGENT_TEXT','AGENT_TOOLS'));

-- +goose Down
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL'));

ALTER TABLE telegram_pending_action
  DROP COLUMN IF EXISTS proposed_description,
  DROP COLUMN IF EXISTS proposed_category_id,
  ALTER COLUMN proposed_transaction_at SET NOT NULL;
