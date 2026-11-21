-- +goose Up
ALTER TABLE intelligence_phase_telemetry ADD COLUMN accepted_dimensions_at_entry TEXT[];

-- +goose Down
ALTER TABLE intelligence_phase_telemetry DROP COLUMN accepted_dimensions_at_entry;
