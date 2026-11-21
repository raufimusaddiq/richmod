-- +goose Up
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL','AGENT_TEXT','AGENT_TOOLS','JUDGMENT','DECISION'));

ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_protocol_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_protocol_check
  CHECK (protocol IN ('responses','chat_completions','systemone'));

-- +goose Down
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_protocol_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_protocol_check
  CHECK (protocol IN ('responses','chat_completions'));

ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL','AGENT_TEXT','AGENT_TOOLS'));
