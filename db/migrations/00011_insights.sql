-- +goose Up
CREATE TABLE insight (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    period DATE NOT NULL CHECK (extract(day FROM period) = 1),
    status TEXT NOT NULL CHECK (status IN ('PENDING','SUCCEEDED','FAILED')),
    input_metrics_json JSONB NOT NULL,
    gateway_route TEXT,
    model TEXT,
    prompt_version TEXT NOT NULL,
    generated_text TEXT,
    confidence NUMERIC(5,4) CHECK (confidence >= 0 AND confidence <= 1),
    data_completeness NUMERIC(5,4) NOT NULL CHECK (data_completeness >= 0 AND data_completeness <= 1),
    requested_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CHECK ((status = 'SUCCEEDED') = (completed_at IS NOT NULL))
);
CREATE INDEX insight_household_created_idx ON insight(household_id,created_at DESC);
CREATE UNIQUE INDEX insight_pending_household_period_unique
    ON insight(household_id,period) WHERE status='PENDING';

-- +goose Down
DROP TABLE insight;
