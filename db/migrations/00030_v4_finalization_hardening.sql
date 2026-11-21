-- +goose Up
CREATE INDEX job_terminal_finished_at_idx ON job(finished_at) WHERE status IN ('SUCCEEDED','FAILED');
CREATE INDEX merchant_alias_household_lower_raw_idx ON merchant_alias(household_id,lower(raw_name));

UPDATE account SET system_key='bank-email-account:'||id::text,updated_at=now()
WHERE system_managed AND system_key LIKE 'bank-email:%';

-- +goose Down
DROP INDEX merchant_alias_household_lower_raw_idx;
DROP INDEX job_terminal_finished_at_idx;
