-- +goose Up
-- One raw merchant spelling must resolve to exactly one learning rule. Keep the
-- oldest alias per (household, case/space-normalized raw name); a group whose
-- rows disagree on merchant or category collapses to a single rule with
-- auto_apply disabled so nothing is guessed. Transaction and evidence rows are
-- never touched; removed alias values are retained in the audit log below.
WITH grouped AS (
    SELECT household_id, lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g')) AS normalized_name,
           bool_or(auto_apply) AS group_auto, bool_and(created_from_user_confirmation) AS group_confirmed,
           count(DISTINCT COALESCE(default_category_id::text, '<null>')) AS category_count,
           count(DISTINCT normalized_merchant_id) AS merchant_count
    FROM merchant_alias GROUP BY household_id, lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g'))
), ranked AS (
    SELECT a.id, a.household_id, g.normalized_name,
           first_value(a.id) OVER (PARTITION BY a.household_id, g.normalized_name ORDER BY a.created_at, a.id) AS keep_id,
           g.group_auto, g.group_confirmed, g.category_count, g.merchant_count
    FROM merchant_alias a JOIN grouped g ON g.household_id=a.household_id
      AND g.normalized_name=lower(regexp_replace(btrim(a.raw_name), '[[:space:]]+', ' ', 'g'))
)
UPDATE merchant_alias a SET auto_apply = ranked.group_auto AND ranked.group_confirmed AND ranked.category_count = 1 AND ranked.merchant_count = 1,
    created_from_user_confirmation = ranked.group_confirmed AND ranked.category_count = 1 AND ranked.merchant_count = 1
FROM ranked WHERE a.id = ranked.keep_id
  AND (a.auto_apply <> (ranked.group_auto AND ranked.group_confirmed)
       OR a.created_from_user_confirmation <> ranked.group_confirmed
       OR ranked.category_count > 1 OR ranked.merchant_count > 1);

WITH ranked AS (
    SELECT id, household_id, raw_name, normalized_merchant_id, default_category_id, auto_apply,
           created_from_user_confirmation, created_at,
           first_value(id) OVER (PARTITION BY household_id, lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g')) ORDER BY created_at, id) AS keep_id
    FROM merchant_alias
), duplicates AS (SELECT * FROM ranked WHERE id <> keep_id)
INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json)
SELECT household_id,'SYSTEM','MERGE_NORMALIZED_MERCHANT_ALIAS','merchant_alias',keep_id,
       jsonb_build_object('removed_alias_id',id,'raw_name',raw_name,
         'normalized_merchant_id',normalized_merchant_id,'default_category_id',default_category_id,
         'auto_apply',auto_apply,'created_from_user_confirmation',created_from_user_confirmation)
FROM duplicates;

WITH ranked AS (
    SELECT id, first_value(id) OVER (PARTITION BY household_id, lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g')) ORDER BY created_at, id) AS keep_id
    FROM merchant_alias
)
DELETE FROM merchant_alias a USING ranked WHERE a.id = ranked.id AND ranked.id <> ranked.keep_id;

CREATE UNIQUE INDEX merchant_alias_household_normalized_raw_unique
    ON merchant_alias(household_id, lower(regexp_replace(btrim(raw_name), '[[:space:]]+', ' ', 'g')));

-- +goose Down
DROP INDEX IF EXISTS merchant_alias_household_normalized_raw_unique;
