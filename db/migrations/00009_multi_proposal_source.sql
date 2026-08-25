-- +goose Up
ALTER TABLE transaction_proposal
    ADD COLUMN proposal_key TEXT NOT NULL DEFAULT 'primary'
    CHECK (proposal_key ~ '^[a-z0-9][a-z0-9:_-]{0,127}$');

ALTER TABLE transaction_proposal
    DROP CONSTRAINT transaction_proposal_source_event_id_key;

ALTER TABLE transaction_proposal
    ADD CONSTRAINT transaction_proposal_source_key_unique
    UNIQUE (source_event_id, proposal_key);

-- +goose Down
ALTER TABLE transaction_proposal
    DROP CONSTRAINT transaction_proposal_source_key_unique;

-- A down migration is intentionally guarded because collapsing multiple valid
-- screenshot rows into one proposal would destroy financial evidence.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM transaction_proposal
        GROUP BY source_event_id HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot remove proposal_key while a source has multiple proposals';
    END IF;
END $$;

ALTER TABLE transaction_proposal
    DROP COLUMN proposal_key;

ALTER TABLE transaction_proposal
    ADD CONSTRAINT transaction_proposal_source_event_id_key UNIQUE (source_event_id);
