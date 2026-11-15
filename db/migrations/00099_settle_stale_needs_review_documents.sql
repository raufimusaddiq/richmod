-- +goose Up
-- N5 (audit v2): a document row can stay status='NEEDS_REVIEW' after the last
-- thing waiting on it was settled (its transaction confirmed, its review item
-- resolved by a path that did not refresh the document). The card then reads
-- "Perlu ditinjau" with nothing to act on. The API now derives the badge from
-- actionable pending work; this migration settles the stored status of rows in
-- that state: NEEDS_REVIEW with nothing actionable reachable from the document or
-- any of its album page sources (open review item, NEEDS_REVIEW transaction or
-- proposal, pending provider-email observation, open transfer case, pending
-- wealth observation). The predicate is the same text as documentPendingSQL in
-- apps/api/internal/document/handler.go; a test keeps the two equal.
--
-- No row id is named. Each changed document gets one audit_log row holding its
-- prior status, which Down uses. Evidence, transactions and review items are not
-- touched. Re-running is a no-op (the rows no longer match). The optional session
-- setting richmod.repair_household narrows the repair to one household.
-- +goose StatementBegin
DO $$
DECLARE
    scope uuid := NULLIF(current_setting('richmod.repair_household', true), '')::uuid;
BEGIN
    WITH stale AS (
        SELECT d.id, d.household_id FROM document d
        WHERE d.status = 'NEEDS_REVIEW' AND (scope IS NULL OR d.household_id = scope)
          AND NOT (
			EXISTS(SELECT 1 FROM review_item ri WHERE ri.household_id=d.household_id AND ri.status IN ('OPEN','PENDING_SEND') AND (ri.document_id=d.id OR ri.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id) OR ri.transaction_id IN (SELECT te.transaction_id FROM transaction_evidence te WHERE te.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id) AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE') OR ri.proposal_id IN (SELECT p.id FROM transaction_proposal p WHERE p.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id)) OR ri.wealth_observation_id IN (SELECT w.id FROM wealth_observation w WHERE w.document_id=d.id) OR ri.financial_email_observation_id IN (SELECT f.id FROM financial_email_observation f WHERE f.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id))))
			OR EXISTS(SELECT 1 FROM transaction_evidence te JOIN transaction t ON t.id=te.transaction_id WHERE t.household_id=d.household_id AND t.status='NEEDS_REVIEW' AND (te.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id) OR te.metadata_json->>'disputed_document_id'=d.id::text) AND COALESCE(te.metadata_json->>'link_status','ACTIVE')='ACTIVE')
			OR EXISTS(SELECT 1 FROM transaction_proposal p WHERE p.household_id=d.household_id AND p.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id) AND p.proposal_status='NEEDS_REVIEW')
			OR EXISTS(SELECT 1 FROM financial_email_observation f WHERE f.household_id=d.household_id AND f.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id) AND f.status IN ('PENDING','REVIEW'))
			OR EXISTS(SELECT 1 FROM transfer_reconciliation_case c WHERE c.household_id=d.household_id AND c.source_event_id IN (SELECT d.source_event_id UNION SELECT dp.source_event_id FROM document_page dp WHERE dp.document_id=d.id) AND c.status='OPEN')
			OR EXISTS(SELECT 1 FROM wealth_observation w WHERE w.household_id=d.household_id AND w.document_id=d.id AND w.status='PENDING'))
    ), changed AS (
        UPDATE document d SET status = 'EXTRACTED', updated_at = now() FROM stale s WHERE d.id = s.id AND d.status = 'NEEDS_REVIEW' RETURNING d.id, d.household_id
    )
    INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
    SELECT household_id, 'SYSTEM', 'REPAIR_STALE_DOCUMENT_REVIEW', 'document', id,
           jsonb_build_object('status', 'NEEDS_REVIEW'),
           jsonb_build_object('status', 'EXTRACTED', 'migration', '00099_settle_stale_needs_review_documents')
    FROM changed;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Restores only documents this migration settled and that are still EXTRACTED.
-- +goose StatementBegin
DO $$
DECLARE
    scope uuid := NULLIF(current_setting('richmod.repair_household', true), '')::uuid;
BEGIN
    WITH restored AS (
        UPDATE document d SET status = 'NEEDS_REVIEW', updated_at = now()
        FROM audit_log a
        WHERE a.action = 'REPAIR_STALE_DOCUMENT_REVIEW' AND a.after_json->>'migration' = '00099_settle_stale_needs_review_documents'
          AND a.entity_id = d.id AND d.status = 'EXTRACTED' AND (scope IS NULL OR a.household_id = scope)
        RETURNING d.id, d.household_id
    )
    INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
    SELECT household_id, 'SYSTEM', 'ROLLBACK_REPAIR_STALE_DOCUMENT_REVIEW', 'document', id,
           '{}'::jsonb, jsonb_build_object('migration', '00099_settle_stale_needs_review_documents')
    FROM restored;
END;
$$;
-- +goose StatementEnd
