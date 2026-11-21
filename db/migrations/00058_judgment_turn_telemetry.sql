-- +goose Up
CREATE TABLE judgment_turn_telemetry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID REFERENCES household(id),
    source_event_id UUID REFERENCES source_event(id),
    lane TEXT NOT NULL CHECK (lane IN ('JEV_ONLY', 'JEV_THEN_GENERATIVE', 'GENERATIVE_ONLY')),
    decision_tasks TEXT[] NOT NULL DEFAULT '{}',
    policy_version TEXT NOT NULL,
    model TEXT,
    native_tool_calls_avoided INTEGER NOT NULL DEFAULT 0 CHECK (native_tool_calls_avoided >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_judgment_turn_telemetry_household_created ON judgment_turn_telemetry(household_id, created_at DESC);
CREATE INDEX idx_judgment_turn_telemetry_lane_created ON judgment_turn_telemetry(lane, created_at DESC);
CREATE INDEX idx_judgment_turn_telemetry_source_event ON judgment_turn_telemetry(source_event_id) WHERE source_event_id IS NOT NULL;

-- +goose Down
DROP TABLE judgment_turn_telemetry;
