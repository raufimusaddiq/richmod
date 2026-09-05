-- +goose Up
DELETE FROM job
WHERE type IN ('PROCESS_GMAIL_HISTORY', 'RENEW_GMAIL_WATCH');

DROP TABLE IF EXISTS gmail_oauth_state;
DROP TABLE IF EXISTS gmail_integration;

-- +goose Down
-- Gmail runtime removal is intentionally irreversible. Historical financial
-- source events and evidence remain in their canonical tables.
SELECT 1;
