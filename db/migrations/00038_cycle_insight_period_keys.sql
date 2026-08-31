-- +goose Up
ALTER TABLE insight DROP CONSTRAINT insight_period_check;

DROP INDEX insight_pending_household_period_unique;
CREATE UNIQUE INDEX insight_pending_household_period_unique
ON insight (
    household_id,
    period,
    (input_metrics_json->>'period_kind'),
    (input_metrics_json->>'period_start')
)
WHERE status='PENDING';

-- +goose Down
DROP INDEX insight_pending_household_period_unique;
CREATE UNIQUE INDEX insight_pending_household_period_unique
ON insight (household_id, period)
WHERE status='PENDING';

ALTER TABLE insight
ADD CONSTRAINT insight_period_check CHECK (EXTRACT(day FROM period)=1);
