-- +goose Up
-- Durable, bounded public Telegram context. This is not financial evidence.
CREATE TABLE telegram_conversation_turn (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  household_id UUID NOT NULL REFERENCES household(id),
  telegram_user_id BIGINT NOT NULL,
  telegram_chat_id BIGINT NOT NULL,
  source_event_id UUID REFERENCES source_event(id),
  role TEXT NOT NULL CHECK (role IN ('USER','ASSISTANT','TOOL')),
  message_text TEXT,
  tool_name TEXT,
  public_context_json JSONB,
  telegram_message_id BIGINT,
  delivery_status TEXT NOT NULL DEFAULT 'N/A' CHECK (delivery_status IN ('N/A','PENDING','SENT','FAILED')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((role='TOOL' AND tool_name IS NOT NULL) OR role<>'TOOL')
);
CREATE INDEX telegram_conversation_turn_household_chat_time_idx ON telegram_conversation_turn(household_id,telegram_chat_id,created_at DESC);
CREATE INDEX telegram_conversation_turn_user_chat_time_idx ON telegram_conversation_turn(telegram_user_id,telegram_chat_id,created_at DESC);
CREATE INDEX telegram_conversation_turn_source_event_idx ON telegram_conversation_turn(source_event_id);

ALTER TABLE job DROP CONSTRAINT IF EXISTS job_lane_check;
ALTER TABLE job ADD CONSTRAINT job_lane_check CHECK (lane IN ('INTERACTIVE','CHAT','DEFAULT','BACKGROUND'));
CREATE INDEX job_chat_claim_idx ON job(run_after,created_at) WHERE lane='CHAT' AND status='PENDING';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_job_lane() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.lane := CASE
    WHEN NEW.type IN ('PROCESS_TELEGRAM_CALLBACK','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','COMPLETE_BANK_REVIEW') THEN 'INTERACTIVE'
    WHEN NEW.type IN ('PROCESS_TELEGRAM_TEXT','PROCESS_TELEGRAM_REVIEW_TEXT') THEN 'CHAT'
    WHEN NEW.type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL') THEN 'BACKGROUND'
    ELSE 'DEFAULT'
  END;
  RETURN NEW;
END $$;
-- +goose StatementEnd

UPDATE job SET lane=CASE
  WHEN type IN ('PROCESS_TELEGRAM_CALLBACK','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','COMPLETE_BANK_REVIEW') THEN 'INTERACTIVE'
  WHEN type IN ('PROCESS_TELEGRAM_TEXT','PROCESS_TELEGRAM_REVIEW_TEXT') THEN 'CHAT'
  WHEN type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL') THEN 'BACKGROUND'
  ELSE 'DEFAULT'
END WHERE status IN ('PENDING','RUNNING');

ALTER TABLE llm_call ADD COLUMN call_kind TEXT NOT NULL DEFAULT 'NATIVE_TOOL';
ALTER TABLE llm_call ADD COLUMN tool_name TEXT;
ALTER TABLE llm_call ADD CONSTRAINT llm_call_kind_check CHECK (call_kind IN ('NATIVE_TOOL'));

-- +goose Down
ALTER TABLE llm_call DROP CONSTRAINT IF EXISTS llm_call_kind_check;
ALTER TABLE llm_call DROP COLUMN IF EXISTS tool_name;
ALTER TABLE llm_call DROP COLUMN IF EXISTS call_kind;
DROP INDEX IF EXISTS job_chat_claim_idx;
UPDATE job SET lane='DEFAULT' WHERE lane='CHAT';
ALTER TABLE job DROP CONSTRAINT IF EXISTS job_lane_check;
ALTER TABLE job ADD CONSTRAINT job_lane_check CHECK (lane IN ('INTERACTIVE','DEFAULT','BACKGROUND'));
DROP INDEX IF EXISTS telegram_conversation_turn_source_event_idx;
DROP INDEX IF EXISTS telegram_conversation_turn_user_chat_time_idx;
DROP INDEX IF EXISTS telegram_conversation_turn_household_chat_time_idx;
DROP TABLE IF EXISTS telegram_conversation_turn;
