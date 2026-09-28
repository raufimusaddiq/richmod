-- +goose Up
-- NULL means the accepted set was not captured; '{}' means explicitly none.
ALTER TABLE intelligence_phase_telemetry ADD COLUMN accepted_dimensions_at_entry TEXT[];

-- +goose Down
ALTER TABLE intelligence_phase_telemetry DROP COLUMN accepted_dimensions_at_entry;
