-- +goose Up
-- The bounded decision plane reports its transport calls as call_kind
-- 'JUDGMENT' and each consumed decision's product outcome as 'DECISION'
-- (apps/worker/internal/telegram/judgment_policy.go). The llm_call_kind_check
-- from 00055 never learned those values, so every judgment metric write was
-- rejected by PostgreSQL and only logged as a worker warning: per-task review
-- rate and Jev latency were silently empty in production (PRD §17/§23).
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check
  CHECK (call_kind IN ('NATIVE_TOOL','AGENT_TEXT','AGENT_TOOLS','JUDGMENT','DECISION'));

-- Same story for the transport column: 00029 allowed only the two generative
-- protocols, but a bounded call is reported as protocol 'systemone'.
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
