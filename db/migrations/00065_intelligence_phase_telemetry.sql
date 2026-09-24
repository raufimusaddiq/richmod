-- IR-09: privacy-safe per-inference phase facts, correlated to source events.
-- Existing llm_call remains the operational token/cost record.
-- +goose Up
CREATE TABLE intelligence_phase_telemetry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID REFERENCES household(id),
    source_event_id UUID REFERENCES source_event(id),
    capability TEXT NOT NULL CHECK (capability IN ('JEV','GENERATIVE')),
    purpose TEXT NOT NULL CHECK (purpose IN ('ROUTE','TRANSACTION_BOUNDED','EXTRACTION','RESIDUAL_CATEGORY','RESIDUAL_REVIEW_ACTION','EVIDENCE_SUPPORT','DOCUMENT_REPAIR','OTHER_BOUNDED')),
    semantic_dimensions TEXT[] NOT NULL DEFAULT '{}',
    answered_dimensions TEXT[] NOT NULL DEFAULT '{}',
    residual_dimensions TEXT[] NOT NULL DEFAULT '{}',
    policy_version TEXT,
    model TEXT,
    latency_ms BIGINT NOT NULL CHECK (latency_ms >= 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('SUCCEEDED','FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_intelligence_phase_event_created ON intelligence_phase_telemetry(source_event_id,created_at) WHERE source_event_id IS NOT NULL;
CREATE INDEX idx_intelligence_phase_household_created ON intelligence_phase_telemetry(household_id,created_at DESC);

-- +goose Down
DROP TABLE intelligence_phase_telemetry;
