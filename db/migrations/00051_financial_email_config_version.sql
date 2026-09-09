-- +goose Up
ALTER TABLE financial_email_source ADD COLUMN config_version INTEGER NOT NULL DEFAULT 1 CHECK (config_version > 0);
ALTER TABLE financial_email_preview ADD COLUMN source_config_version INTEGER;
UPDATE financial_email_preview p SET source_config_version=s.config_version FROM financial_email_source s WHERE s.id=p.financial_source_id;
ALTER TABLE financial_email_preview ALTER COLUMN source_config_version SET NOT NULL;

-- +goose Down
ALTER TABLE financial_email_preview DROP COLUMN source_config_version;
ALTER TABLE financial_email_source DROP COLUMN config_version;
