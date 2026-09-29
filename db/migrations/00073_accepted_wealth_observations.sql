-- Accepted observations are visible independently from complete snapshots.
-- They never imply a snapshot or alter snapshot-derived net worth.
-- +goose Up
ALTER TABLE wealth_observation DROP CONSTRAINT wealth_observation_status_check;
ALTER TABLE wealth_observation ADD CONSTRAINT wealth_observation_status_check CHECK (status IN ('PENDING','ACCEPTED','APPLIED','DISMISSED'));

-- +goose Down
UPDATE wealth_observation SET status='PENDING' WHERE status='ACCEPTED';
ALTER TABLE wealth_observation DROP CONSTRAINT wealth_observation_status_check;
ALTER TABLE wealth_observation ADD CONSTRAINT wealth_observation_status_check CHECK (status IN ('PENDING','APPLIED','DISMISSED'));
