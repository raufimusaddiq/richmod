-- +goose Up
CREATE TEMP TABLE merchant_identity_merge ON COMMIT DROP AS
WITH ranked AS (
    SELECT
        id,
        first_value(id) OVER (
            PARTITION BY household_id, lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g'))
            ORDER BY (normalized_name = upper(normalized_name)), created_at, id
        ) AS keep_id
    FROM merchant
)
SELECT id AS old_id, keep_id
FROM ranked
WHERE id <> keep_id;

UPDATE transaction t
SET merchant_id = m.keep_id,
    updated_at = now()
FROM merchant_identity_merge m
WHERE t.merchant_id = m.old_id;

UPDATE merchant_alias a
SET normalized_merchant_id = m.keep_id
FROM merchant_identity_merge m
WHERE a.normalized_merchant_id = m.old_id;

DELETE FROM merchant m
USING merchant_identity_merge merge
WHERE m.id = merge.old_id;

UPDATE merchant
SET normalized_name = regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g'),
    updated_at = now()
WHERE normalized_name <> regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g');

ALTER TABLE merchant
    ADD CONSTRAINT merchant_normalized_name_canonical
    CHECK (normalized_name = regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g'));

CREATE UNIQUE INDEX merchant_household_normalized_identity_unique
    ON merchant(household_id, lower(regexp_replace(btrim(normalized_name), '[[:space:]]+', ' ', 'g')));

-- +goose Down
DROP INDEX IF EXISTS merchant_household_normalized_identity_unique;
ALTER TABLE merchant DROP CONSTRAINT IF EXISTS merchant_normalized_name_canonical;
