-- +goose Up
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
 (financial_email_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL) OR
 (wealth_observation_id IS NOT NULL AND financial_email_observation_id IS NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL))
);

-- +goose Down
DELETE FROM review_item WHERE financial_email_observation_id IS NOT NULL AND wealth_observation_id IS NOT NULL;
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
 (financial_email_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL) OR
 (wealth_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL AND financial_email_observation_id IS NULL) OR
 (cycle_residual_case_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL))
);
