-- +goose Up
-- Bounded semantic decision provenance (PRD §15). Stores which policy consumed
-- which bounded answer, not raw financial content.
CREATE TABLE judgment_decision (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_event_id UUID REFERENCES source_event(id),
    task TEXT NOT NULL,
    model TEXT,
    policy_version TEXT NOT NULL,
    question_keys TEXT[] NOT NULL DEFAULT '{}',
    answer_summary_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    outcome TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_judgment_decision_household_created ON judgment_decision(household_id, created_at DESC);
CREATE INDEX idx_judgment_decision_task_outcome ON judgment_decision(task, outcome, created_at DESC);
CREATE INDEX idx_judgment_decision_source_event ON judgment_decision(source_event_id) WHERE source_event_id IS NOT NULL;

-- +goose Down
DROP TABLE judgment_decision;
