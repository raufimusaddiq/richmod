-- +goose Up
ALTER TABLE bank_email_extraction ADD COLUMN notice_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE bank_email_extraction DROP COLUMN notice_at;
