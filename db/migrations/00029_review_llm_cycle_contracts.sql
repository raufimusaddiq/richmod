-- +goose Up
ALTER TABLE review_item ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE review_item ADD COLUMN resolved_by_user_id UUID REFERENCES "user"(id);
ALTER TABLE review_item ADD COLUMN resolution_action TEXT;
ALTER TABLE review_item ADD COLUMN resolution_values JSONB;

CREATE UNIQUE INDEX review_item_active_transaction_unique ON review_item(transaction_id) WHERE transaction_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN');
CREATE UNIQUE INDEX review_item_active_proposal_unique ON review_item(proposal_id) WHERE proposal_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN');
CREATE UNIQUE INDEX review_item_active_source_unique ON review_item(source_event_id) WHERE source_event_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN');
CREATE UNIQUE INDEX review_item_active_document_unique ON review_item(document_id) WHERE document_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN');

CREATE TABLE llm_call (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  household_id UUID REFERENCES household(id),
  task TEXT NOT NULL,
  protocol TEXT NOT NULL CHECK (protocol IN ('responses','chat_completions')),
  model TEXT,
  status TEXT NOT NULL CHECK (status IN ('SUCCEEDED','FAILED')),
  error_class TEXT,
  duration_ms BIGINT NOT NULL CHECK (duration_ms >= 0),
  input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  cost NUMERIC(20,8),
  attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX llm_call_household_task_time_idx ON llm_call(household_id,task,created_at DESC);

-- The end date is exclusive. Null bounds mean no confirmed primary salary cycle.
-- +goose StatementBegin
CREATE FUNCTION salary_cycle_bounds(p_household UUID, p_as_of DATE)
RETURNS TABLE(configured BOOLEAN, starts_on DATE, ends_on DATE)
LANGUAGE sql STABLE AS $$
  WITH anchors AS (
    SELECT se.pay_date
    FROM salary_event se JOIN salary_source ss ON ss.id=se.salary_source_id
    WHERE se.household_id=p_household AND ss.active AND ss.is_primary AND se.status='CONFIRMED'
  )
  SELECT EXISTS(SELECT 1 FROM salary_source WHERE household_id=p_household AND active AND is_primary),
         (SELECT max(pay_date) FROM anchors WHERE pay_date<=p_as_of),
         (SELECT min(pay_date) FROM anchors WHERE pay_date>p_as_of)
$$;
-- +goose StatementEnd

-- Ensure incomplete sources already visible as NEEDS_REVIEW have one canonical item.
INSERT INTO review_item(household_id,source_event_id,review_type,status)
SELECT s.household_id,s.id,CASE WHEN s.source_type='BANK_EMAIL' THEN 'UNKNOWN_BANK_TEMPLATE' ELSE 'MANUAL_CORRECTION' END,'OPEN'
FROM source_event s
WHERE s.processing_status='NEEDS_REVIEW'
  AND NOT EXISTS(SELECT 1 FROM review_item ri WHERE ri.source_event_id=s.id AND ri.status IN ('PENDING_SEND','OPEN'));

-- +goose Down
DROP FUNCTION IF EXISTS salary_cycle_bounds(UUID,DATE);
DROP TABLE IF EXISTS llm_call;
DROP INDEX IF EXISTS review_item_active_document_unique;
DROP INDEX IF EXISTS review_item_active_source_unique;
DROP INDEX IF EXISTS review_item_active_proposal_unique;
DROP INDEX IF EXISTS review_item_active_transaction_unique;
ALTER TABLE review_item DROP COLUMN IF EXISTS resolution_values;
ALTER TABLE review_item DROP COLUMN IF EXISTS resolution_action;
ALTER TABLE review_item DROP COLUMN IF EXISTS resolved_by_user_id;
ALTER TABLE review_item DROP COLUMN IF EXISTS updated_at;
