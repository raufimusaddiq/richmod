-- +goose Up
ALTER TABLE review_item ADD COLUMN financial_email_observation_id UUID REFERENCES financial_email_observation(id);
ALTER TABLE transfer_reconciliation_case DROP CONSTRAINT transfer_reconciliation_case_source_event_id_key;
ALTER TABLE transfer_reconciliation_case ADD COLUMN financial_email_observation_id UUID REFERENCES financial_email_observation(id);
CREATE UNIQUE INDEX transfer_reconciliation_case_source_unique ON transfer_reconciliation_case(source_event_id) WHERE financial_email_observation_id IS NULL;
CREATE UNIQUE INDEX transfer_reconciliation_case_observation_unique ON transfer_reconciliation_case(financial_email_observation_id) WHERE financial_email_observation_id IS NOT NULL;
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
 (financial_email_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL) OR
 (wealth_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL AND financial_email_observation_id IS NULL) OR
 (cycle_residual_case_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL))
);

-- +goose Down
DELETE FROM review_item WHERE financial_email_observation_id IS NOT NULL;
DELETE FROM transfer_reconciliation_case WHERE financial_email_observation_id IS NOT NULL;
DROP INDEX transfer_reconciliation_case_observation_unique;
DROP INDEX transfer_reconciliation_case_source_unique;
ALTER TABLE transfer_reconciliation_case DROP COLUMN financial_email_observation_id;
ALTER TABLE transfer_reconciliation_case ADD CONSTRAINT transfer_reconciliation_case_source_event_id_key UNIQUE(source_event_id);
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
 (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND source_event_id IS NULL AND document_id IS NULL AND wealth_observation_id IS NULL) OR
 (wealth_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND source_event_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NULL AND wealth_observation_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL))
);
ALTER TABLE review_item DROP COLUMN financial_email_observation_id;
