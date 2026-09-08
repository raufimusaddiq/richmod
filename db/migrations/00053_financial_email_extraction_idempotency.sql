-- +goose Up
ALTER TABLE financial_email_event
    ADD COLUMN extraction_status TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (extraction_status IN ('PENDING','SUCCEEDED')),
    ADD COLUMN extracted_at TIMESTAMPTZ,
    ADD COLUMN observation_count INTEGER CHECK (observation_count >= 0),
    ADD COLUMN extraction_model TEXT;

UPDATE financial_email_event fe
SET extraction_status='SUCCEEDED',
    extracted_at=now(),
    observation_count=(SELECT count(*) FROM financial_email_observation o WHERE o.source_event_id=fe.source_event_id),
    extraction_model='legacy'
FROM source_event se
WHERE se.id=fe.source_event_id
  AND (se.processing_status IN ('PROCESSED','NEEDS_REVIEW','IGNORED')
       OR EXISTS (SELECT 1 FROM financial_email_observation o WHERE o.source_event_id=fe.source_event_id));

CREATE UNIQUE INDEX wealth_observation_financial_email_unique
    ON wealth_observation(financial_email_observation_id)
    WHERE financial_email_observation_id IS NOT NULL;

-- +goose Down
DROP INDEX wealth_observation_financial_email_unique;
ALTER TABLE financial_email_event
    DROP COLUMN extraction_model,
    DROP COLUMN observation_count,
    DROP COLUMN extracted_at,
    DROP COLUMN extraction_status;
