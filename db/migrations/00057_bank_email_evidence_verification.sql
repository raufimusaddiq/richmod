-- +goose Up
-- Bounded evidence verification for bank email (PRD §20). Additive only: it
-- records what the judgment plane claimed about an already-extracted
-- notification so the extractor's self-reported confidence is no longer the
-- sole semantic gate. No canonical financial state is written here.
CREATE TABLE bank_email_evidence_verification (
    source_event_id UUID PRIMARY KEY REFERENCES source_event(id),
    listener_id UUID REFERENCES bank_email_listener(id),
    bank_email_verification_policy_version TEXT NOT NULL,
    gateway_model TEXT,
    answer_summary_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bank_email_evidence_verification_listener
    ON bank_email_evidence_verification(listener_id, created_at DESC)
    WHERE listener_id IS NOT NULL;

-- +goose Down
DROP TABLE bank_email_evidence_verification;
