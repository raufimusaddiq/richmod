-- +goose Up
-- A financial-email wealth review is bound to the exact staged observation,
-- while retaining the child wealth_observation for the existing snapshot flow.
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
 (financial_email_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL) OR
 (wealth_observation_id IS NOT NULL AND financial_email_observation_id IS NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL))
);

-- +goose Down
-- Rows using both links are removed before restoring the prior constraint.
DELETE FROM review_item WHERE financial_email_observation_id IS NOT NULL AND wealth_observation_id IS NOT NULL;
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
 (financial_email_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
 (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL) OR
 (wealth_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL AND financial_email_observation_id IS NULL) OR
 (cycle_residual_case_id IS NULL AND wealth_observation_id IS NULL AND financial_email_observation_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL))
);
