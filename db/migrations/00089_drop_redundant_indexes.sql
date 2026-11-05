-- +goose Up
-- Drop indexes that another index or constraint on the same table already
-- implies, so they only cost write amplification and space (ADR-058 follow-up).
-- Each keeper serves the same lookups with the same leading columns:
--
--   cycle_residual_allocation_case_idx (cycle_residual_case_id)
--     prefix of cycle_residual_allocation_case_account_unique (cycle_residual_case_id, wealth_account_id)
--   document_page_document_idx (document_id, page_index)
--     duplicate of document_page_document_id_page_index_key
--   review_request_recipient_lookup_idx (telegram_chat_id, telegram_message_id)
--     duplicate of review_request_recipient_telegram_chat_id_telegram_message__key
--   household_member_one_active_household_per_user (user_id) WHERE active
--     implied by household_member_one_household_per_user (user_id), migration 00088
--   telegram_identity_household_id_user_id_key UNIQUE (household_id, user_id)
--     implied by telegram_identity_one_per_user (user_id), migration 00088
--
-- The (household_id, id) unique keys from 00087 look implied by each primary
-- key but are the targets of the composite household foreign keys, so they stay.
-- No code names these indexes or uses them as an ON CONFLICT arbiter.
DROP INDEX cycle_residual_allocation_case_idx;
DROP INDEX document_page_document_idx;
DROP INDEX review_request_recipient_lookup_idx;
DROP INDEX household_member_one_active_household_per_user;
ALTER TABLE telegram_identity DROP CONSTRAINT telegram_identity_household_id_user_id_key;

-- +goose Down
ALTER TABLE telegram_identity
    ADD CONSTRAINT telegram_identity_household_id_user_id_key UNIQUE (household_id, user_id);
CREATE UNIQUE INDEX household_member_one_active_household_per_user
    ON household_member(user_id)
    WHERE active = TRUE;
CREATE INDEX review_request_recipient_lookup_idx ON review_request_recipient(telegram_chat_id, telegram_message_id);
CREATE INDEX document_page_document_idx ON document_page(document_id, page_index);
CREATE INDEX cycle_residual_allocation_case_idx ON cycle_residual_allocation(cycle_residual_case_id);
