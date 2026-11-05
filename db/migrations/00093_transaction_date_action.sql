-- +goose Up
-- MISSING_TRANSACTION_DATE and TRANSACTION_FACTS_MISSING stored the payslip
-- action name SET_PAY_DATE for a transaction date. They now store their own
-- action, SET_TRANSACTION_DATE, plus the bounded one-tap date choices
-- (TODAY, YESTERDAY, MESSAGE_DATE). Only active reviews are rewritten: resolved
-- history keeps the decision the household actually answered. Each rewrite is
-- audited. The application still accepts SET_PAY_DATE on a transaction-bound
-- review during the rolling window (reviewdomain.Executable).
INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,before_json,after_json)
SELECT household_id,'SYSTEM','MIGRATE_REVIEW_DECISION','review_item',id,
       jsonb_build_object('allowedActions',decision->'allowedActions'),
       jsonb_build_object('allowedActions',(SELECT jsonb_agg(CASE WHEN a='SET_PAY_DATE' THEN 'SET_TRANSACTION_DATE' ELSE a END ORDER BY o) FROM jsonb_array_elements_text(decision->'allowedActions') WITH ORDINALITY x(a,o)),'migration','00093')
FROM review_item
WHERE review_type IN ('MISSING_TRANSACTION_DATE','TRANSACTION_FACTS_MISSING')
  AND status IN ('PENDING_SEND','OPEN')
  AND jsonb_typeof(decision->'allowedActions')='array'
  AND decision->'allowedActions' ? 'SET_PAY_DATE';

UPDATE review_item
SET decision = jsonb_set(
      jsonb_set(decision,'{allowedActions}',(SELECT jsonb_agg(CASE WHEN a='SET_PAY_DATE' THEN 'SET_TRANSACTION_DATE' ELSE a END ORDER BY o) FROM jsonb_array_elements_text(decision->'allowedActions') WITH ORDINALITY x(a,o))),
      '{boundedChoices}',
      COALESCE(decision->'boundedChoices','[{"key":"TODAY","label":"Hari ini"},{"key":"YESTERDAY","label":"Kemarin"},{"key":"MESSAGE_DATE","label":"Tanggal pesan"}]'::jsonb)),
    updated_at = now()
WHERE review_type IN ('MISSING_TRANSACTION_DATE','TRANSACTION_FACTS_MISSING')
  AND status IN ('PENDING_SEND','OPEN')
  AND jsonb_typeof(decision->'allowedActions')='array'
  AND decision->'allowedActions' ? 'SET_PAY_DATE';

-- +goose Down
-- Restore the previous action name on active reviews; the bounded choices are
-- additive metadata older code ignores, so they are left in place.
UPDATE review_item
SET decision = jsonb_set(decision,'{allowedActions}',(SELECT jsonb_agg(CASE WHEN a='SET_TRANSACTION_DATE' THEN 'SET_PAY_DATE' ELSE a END ORDER BY o) FROM jsonb_array_elements_text(decision->'allowedActions') WITH ORDINALITY x(a,o))),
    updated_at = now()
WHERE review_type IN ('MISSING_TRANSACTION_DATE','TRANSACTION_FACTS_MISSING')
  AND status IN ('PENDING_SEND','OPEN')
  AND jsonb_typeof(decision->'allowedActions')='array'
  AND decision->'allowedActions' ? 'SET_TRANSACTION_DATE';
