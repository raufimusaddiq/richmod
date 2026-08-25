-- +goose Up
CREATE TABLE reconciliation_merge (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_transaction_id UUID NOT NULL REFERENCES transaction(id),
    target_transaction_id UUID NOT NULL REFERENCES transaction(id),
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'REVERSED')),
    created_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reversed_at TIMESTAMPTZ,
    CHECK (source_transaction_id <> target_transaction_id),
    CHECK ((status = 'REVERSED') = (reversed_at IS NOT NULL))
);
CREATE UNIQUE INDEX reconciliation_merge_active_source_unique
    ON reconciliation_merge (source_transaction_id) WHERE status = 'ACTIVE';

CREATE TABLE reconciliation_merge_evidence (
    merge_id UUID NOT NULL REFERENCES reconciliation_merge(id),
    original_evidence_id UUID NOT NULL REFERENCES transaction_evidence(id),
    copied_evidence_id UUID NOT NULL REFERENCES transaction_evidence(id) ON DELETE CASCADE,
    PRIMARY KEY (merge_id, original_evidence_id),
    UNIQUE (copied_evidence_id)
);

-- +goose Down
DROP TABLE reconciliation_merge_evidence;
DROP TABLE reconciliation_merge;
