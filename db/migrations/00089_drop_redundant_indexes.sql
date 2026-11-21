-- +goose Up
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
