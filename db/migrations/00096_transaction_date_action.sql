-- +goose Up
INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json)
SELECT household_id,'SYSTEM','RESOLVE_DUPLICATE_OPEN_REVIEW','review_item',id,jsonb_build_object('migration','00096','financial_email_observation_id',financial_email_observation_id)
FROM (SELECT id,household_id,financial_email_observation_id,row_number() OVER (PARTITION BY financial_email_observation_id ORDER BY created_at,id) AS n
      FROM review_item WHERE financial_email_observation_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN')) d WHERE n>1;
UPDATE review_item SET status='RESOLVED',resolved_at=now(),resolution_action='DUPLICATE_OPEN_REVIEW',updated_at=now()
WHERE id IN (SELECT id FROM (SELECT id,row_number() OVER (PARTITION BY financial_email_observation_id ORDER BY created_at,id) AS n
      FROM review_item WHERE financial_email_observation_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN')) d WHERE n>1);
UPDATE review_request SET status='RESOLVED',resolved_at=now()
WHERE status IN ('PENDING_SEND','OPEN') AND review_item_id IN (SELECT entity_id FROM audit_log WHERE action='RESOLVE_DUPLICATE_OPEN_REVIEW' AND after_json->>'migration'='00096');
UPDATE review_conversation SET state='RESOLVED',updated_at=now()
WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id IN (SELECT entity_id FROM audit_log WHERE action='RESOLVE_DUPLICATE_OPEN_REVIEW' AND after_json->>'migration'='00096'));
CREATE UNIQUE INDEX review_item_open_financial_email_observation_unique ON review_item(financial_email_observation_id) WHERE financial_email_observation_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN');

INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,before_json,after_json)
SELECT household_id,'SYSTEM','MIGRATE_REVIEW_DECISION','review_item',id,
       jsonb_build_object('allowedActions',decision->'allowedActions'),
       jsonb_build_object('allowedActions',(SELECT jsonb_agg(CASE WHEN a='SET_PAY_DATE' THEN 'SET_TRANSACTION_DATE' ELSE a END ORDER BY o) FROM jsonb_array_elements_text(decision->'allowedActions') WITH ORDINALITY x(a,o)),'migration','00096')
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
DROP INDEX IF EXISTS review_item_open_financial_email_observation_unique;
UPDATE review_item
SET decision = jsonb_set(decision,'{allowedActions}',(SELECT jsonb_agg(CASE WHEN a='SET_TRANSACTION_DATE' THEN 'SET_PAY_DATE' ELSE a END ORDER BY o) FROM jsonb_array_elements_text(decision->'allowedActions') WITH ORDINALITY x(a,o))),
    updated_at = now()
WHERE review_type IN ('MISSING_TRANSACTION_DATE','TRANSACTION_FACTS_MISSING')
  AND status IN ('PENDING_SEND','OPEN')
  AND jsonb_typeof(decision->'allowedActions')='array'
  AND decision->'allowedActions' ? 'SET_TRANSACTION_DATE';
