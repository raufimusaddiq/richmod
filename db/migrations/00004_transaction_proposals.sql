-- +goose Up
CREATE TABLE transaction_proposal (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_event_id UUID NOT NULL REFERENCES source_event(id),
    proposed_type TEXT NOT NULL CHECK (proposed_type IN ('INCOME', 'EXPENSE', 'REFUND')),
    amount NUMERIC(20,0) NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL DEFAULT 'IDR' CHECK (currency = 'IDR'),
    transaction_at TIMESTAMPTZ NOT NULL,
    merchant_raw TEXT,
    counterparty_raw TEXT,
    category_candidate_id UUID REFERENCES category(id),
    description TEXT,
    note TEXT,
    confidence NUMERIC(5,4) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    proposal_status TEXT NOT NULL CHECK (proposal_status IN ('PENDING', 'ACCEPTED', 'REJECTED', 'NEEDS_REVIEW', 'MERGED')),
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_event_id)
);
CREATE INDEX transaction_proposal_review_idx ON transaction_proposal (household_id, created_at DESC) WHERE proposal_status = 'NEEDS_REVIEW';

-- +goose Down
DROP TABLE transaction_proposal;
