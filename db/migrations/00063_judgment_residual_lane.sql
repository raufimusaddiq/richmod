-- +goose Up
ALTER TABLE judgment_turn_telemetry
    DROP CONSTRAINT IF EXISTS judgment_turn_telemetry_lane_check;
ALTER TABLE judgment_turn_telemetry
    ADD CONSTRAINT judgment_turn_telemetry_lane_check CHECK (lane IN ('JEV_ONLY', 'JEV_THEN_GENERATIVE', 'JEV_THEN_GENERATIVE_THEN_RESIDUAL_JEV', 'GENERATIVE_ONLY'));
ALTER TABLE judgment_turn_telemetry
    ADD COLUMN IF NOT EXISTS residual_dimensions TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE judgment_turn_telemetry DROP COLUMN IF EXISTS residual_dimensions;
ALTER TABLE judgment_turn_telemetry DROP CONSTRAINT IF EXISTS judgment_turn_telemetry_lane_check;
ALTER TABLE judgment_turn_telemetry ADD CONSTRAINT judgment_turn_telemetry_lane_check CHECK (lane IN ('JEV_ONLY', 'JEV_THEN_GENERATIVE', 'GENERATIVE_ONLY'));
