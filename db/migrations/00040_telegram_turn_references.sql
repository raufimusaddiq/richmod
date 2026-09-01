-- +goose Up
CREATE TABLE telegram_turn_reference (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  turn_id UUID NOT NULL REFERENCES telegram_conversation_turn(id) ON DELETE CASCADE,
  ref_key TEXT NOT NULL CHECK (ref_key ~ '^(tx|review)_[0-9]+$'),
  entity_type TEXT NOT NULL CHECK (entity_type IN ('TRANSACTION','REVIEW')),
  entity_id UUID NOT NULL,
  household_id UUID NOT NULL REFERENCES household(id),
  telegram_user_id BIGINT NOT NULL,
  telegram_chat_id BIGINT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  UNIQUE(turn_id, ref_key)
);
CREATE INDEX telegram_turn_reference_scope_idx ON telegram_turn_reference(household_id,telegram_user_id,telegram_chat_id,ref_key,expires_at);

ALTER TABLE telegram_pending_action ADD COLUMN proposed_category_id UUID REFERENCES category(id);
ALTER TABLE telegram_pending_action ADD COLUMN proposed_description TEXT;
ALTER TABLE telegram_pending_action ALTER COLUMN proposed_transaction_at DROP NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS telegram_turn_reference_scope_idx;
DROP TABLE IF EXISTS telegram_turn_reference;
ALTER TABLE telegram_pending_action DROP COLUMN IF EXISTS proposed_description;
ALTER TABLE telegram_pending_action DROP COLUMN IF EXISTS proposed_category_id;
ALTER TABLE telegram_pending_action ALTER COLUMN proposed_transaction_at SET NOT NULL;
