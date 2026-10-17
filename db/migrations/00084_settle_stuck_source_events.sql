-- +goose Up
-- Ticket #356: 17 source events show as NEEDS_REVIEW although no active review
-- item can resolve them (detected by the stuck-source-events query, see
-- docs/runbooks/stuck-source-events.md). Each group below has one root cause,
-- found by read-only inspection of production, and one settlement:
--   A  6 Telegram chat replies saved as NEEDS_REVIEW (never a review subject)
--        -> PROCESSED.
--   B  3 bank emails whose extraction was INVALID under pre-ADR-048 code
--        -> FAILED plus the dismissable failed-source Tindakan item that
--        reviewdomain.RecordFailedSourceAction writes today. Not re-extracted.
--   C  1 wealth document whose observation is APPLIED and item RESOLVED
--        -> PROCESSED.
--   D  5 documents whose DOCUMENT_EXTRACTION_LOW_CONFIDENCE item was resolved
--        IGNORE without settling the source -> source IGNORED. The document
--        stays EXTRACTED: document.status has no IGNORED value (see below).
--   E  2 transfer-proof documents whose evidence a manual unlink removed
--        (audit_log MANUAL_UNLINK_EVIDENCE) -> reprocessed through the normal
--        PROCESS_TRANSACTION_SCREENSHOT job; no evidence row is written here.
--
-- No row id is named. Each group is found by the state signature the inspection
-- established among events received before 2026-10-07 (the batch inspected;
-- group A's cause is still live in code, so later events must neither be
-- touched nor block this deploy), and the migration refuses to run unless the
-- group has exactly its expected size (A 6, B 3, C 1, D 5, E 2) or is empty, so
-- a database that differs from the inspected one fails loudly instead of being
-- half-repaired. The five groups are all-or-nothing: if some match and others
-- match nothing, the whole migration raises. Events at or after the cutoff stay visible to the detection
-- query for their own follow-up.
-- An empty group is a NOTICE and a no-op: fresh and already-repaired databases
-- migrate cleanly and re-running is idempotent. The optional session setting
-- richmod.repair_household narrows every group to one household; it is unset
-- in a normal migrate.
--
-- Note on D: reviewdomain.ResolveDocumentReview writes document.status='IGNORED',
-- which document_status_check forbids; that is a separate defect tracked outside
-- this repair, so the migration does not depend on it.
--
-- Every changed event gets one audit_log row (REPAIR_STUCK_SOURCE_EVENT) holding
-- the prior state, which Down uses to restore it. No transaction, evidence or
-- review row is created, changed or deleted. For group E the migration only
-- retires the two stale NEEDS_REVIEW proposals (REJECTED, key prefixed
-- 'superseded-' so the reprocess can create row-001 again), resets the document
-- to CLASSIFIED, marks the source PROCESSING and queues the job. After the worker
-- runs, verify the matched transaction is still one canonical row with the
-- steps in docs/runbooks/stuck-source-events.md.
-- +goose StatementBegin
DO $$
DECLARE
    scope uuid := NULLIF(current_setting('richmod.repair_household', true), '')::uuid;
    ids uuid[];
    n int;
    changed int;
    settled text := '';
    cutoff CONSTANT timestamptz := '2026-10-07 00:00:00+07';
    mig CONSTANT text := '00084_settle_stuck_source_events';
BEGIN
    -- A: chat replies parked as NEEDS_REVIEW.
    SELECT array_agg(se.id) INTO ids FROM source_event se
    WHERE (scope IS NULL OR se.household_id = scope) AND se.received_at < cutoff
      AND se.source_type = 'TELEGRAM_TEXT' AND se.processing_status = 'NEEDS_REVIEW' AND se.parser_name IS NULL
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM document d WHERE d.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM telegram_pending_batch b WHERE b.source_event_id = se.id AND b.status = 'PENDING');
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        RAISE NOTICE '00084 group A: nothing to settle';
    ELSIF n <> 6 THEN
        RAISE EXCEPTION '00084 group A: % events match, expected 6; resolve by hand', n;
    ELSE
        settled := settled || 'A';
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        SELECT household_id, 'SYSTEM', 'REPAIR_STUCK_SOURCE_EVENT', 'source_event', id,
               jsonb_build_object('group', 'A', 'processing_status', 'NEEDS_REVIEW'),
               jsonb_build_object('group', 'A', 'processing_status', 'PROCESSED', 'migration', mig, 'ticket', '356')
        FROM source_event WHERE id = ANY(ids);
        UPDATE source_event SET processing_status = 'PROCESSED' WHERE id = ANY(ids) AND processing_status = 'NEEDS_REVIEW';
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> n THEN RAISE EXCEPTION '00084 group A: % of % rows changed', changed, n; END IF;
    END IF;

    -- B: bank emails with an INVALID extraction recorded as NEEDS_REVIEW.
    SELECT array_agg(se.id) INTO ids FROM source_event se
    WHERE (scope IS NULL OR se.household_id = scope) AND se.received_at < cutoff
      AND se.source_type = 'BANK_EMAIL' AND se.processing_status = 'NEEDS_REVIEW'
      AND EXISTS (SELECT 1 FROM bank_email_extraction x WHERE x.source_event_id = se.id AND x.validation_status = 'INVALID' AND x.policy_result = 'NEEDS_REVIEW')
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id);
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        RAISE NOTICE '00084 group B: nothing to settle';
    ELSIF n <> 3 THEN
        RAISE EXCEPTION '00084 group B: % events match, expected 3; resolve by hand', n;
    ELSE
        settled := settled || 'B';
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        SELECT household_id, 'SYSTEM', 'REPAIR_STUCK_SOURCE_EVENT', 'source_event', id,
               jsonb_build_object('group', 'B', 'processing_status', 'NEEDS_REVIEW'),
               jsonb_build_object('group', 'B', 'processing_status', 'FAILED', 'migration', mig, 'ticket', '356')
        FROM source_event WHERE id = ANY(ids);
        UPDATE source_event SET processing_status = 'FAILED' WHERE id = ANY(ids) AND processing_status = 'NEEDS_REVIEW';
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> n THEN RAISE EXCEPTION '00084 group B: % of % rows changed', changed, n; END IF;
        -- Same item reviewdomain.RecordFailedSourceAction writes for a BANK_EMAIL / INVALID source.
        INSERT INTO integration_action(household_id, integration_type, action_type, status, title, description, dedupe_key, metadata_json)
        SELECT se.household_id, 'SOURCE_PROCESSING', 'SOURCE_FAILED', 'OPEN',
               'Email bank tidak bisa dibaca',
               'Hasil pembacaan email tidak bisa dipakai, jadi belum ada transaksi yang dicatat. Periksa email aslinya dan catat transaksinya manual bila perlu, lalu tutup tindakan ini.',
               se.id::text,
               jsonb_build_object('source_event_id', se.id::text, 'source_type', 'BANK_EMAIL', 'reason', 'INVALID', 'repair_migration', mig)
        FROM source_event se WHERE se.id = ANY(ids)
        ON CONFLICT (household_id, integration_type, action_type, dedupe_key) DO NOTHING;
    END IF;

    -- C: document-sourced wealth observation applied, item resolved, source never settled.
    SELECT array_agg(se.id) INTO ids FROM source_event se
    WHERE (scope IS NULL OR se.household_id = scope) AND se.received_at < cutoff
      AND se.processing_status = 'NEEDS_REVIEW'
      AND EXISTS (SELECT 1 FROM document d JOIN wealth_observation w ON w.document_id = d.id
                  WHERE (d.source_event_id = se.id OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
                    AND d.document_type = 'WEALTH_OBSERVATION' AND w.status = 'APPLIED')
      AND NOT EXISTS (SELECT 1 FROM review_item ri JOIN wealth_observation w ON w.id = ri.wealth_observation_id JOIN document d ON d.id = w.document_id
                      WHERE (d.source_event_id = se.id OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
                        AND ri.status IN ('OPEN','PENDING_SEND'))
      AND NOT EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id AND p.proposal_status = 'NEEDS_REVIEW')
      AND NOT EXISTS (SELECT 1 FROM transaction_evidence te JOIN transaction t ON t.id = te.transaction_id WHERE te.source_event_id = se.id AND t.status = 'NEEDS_REVIEW');
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        RAISE NOTICE '00084 group C: nothing to settle';
    ELSIF n <> 1 THEN
        RAISE EXCEPTION '00084 group C: % events match, expected 1; resolve by hand', n;
    ELSE
        settled := settled || 'C';
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        SELECT household_id, 'SYSTEM', 'REPAIR_STUCK_SOURCE_EVENT', 'source_event', id,
               jsonb_build_object('group', 'C', 'processing_status', 'NEEDS_REVIEW'),
               jsonb_build_object('group', 'C', 'processing_status', 'PROCESSED', 'migration', mig, 'ticket', '356')
        FROM source_event WHERE id = ANY(ids);
        UPDATE source_event SET processing_status = 'PROCESSED' WHERE id = ANY(ids) AND processing_status = 'NEEDS_REVIEW';
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> n THEN RAISE EXCEPTION '00084 group C: % of % rows changed', changed, n; END IF;
    END IF;

    -- D: documents whose only review items were resolved IGNORE.
    SELECT array_agg(se.id) INTO ids FROM source_event se
    WHERE (scope IS NULL OR se.household_id = scope) AND se.received_at < cutoff
      AND se.processing_status = 'NEEDS_REVIEW'
      AND EXISTS (SELECT 1 FROM document d WHERE (d.source_event_id = se.id OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id)) AND d.status = 'EXTRACTED')
      AND EXISTS (SELECT 1 FROM review_item ri JOIN document d ON d.id = ri.document_id
                  WHERE d.source_event_id = se.id OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
      AND NOT EXISTS (SELECT 1 FROM review_item ri JOIN document d ON d.id = ri.document_id
                      WHERE (d.source_event_id = se.id OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
                        AND NOT (ri.status = 'RESOLVED' AND ri.resolution_action = 'IGNORE' AND ri.review_type = 'DOCUMENT_EXTRACTION_LOW_CONFIDENCE'))
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM wealth_observation w JOIN document d ON d.id = w.document_id
                      WHERE d.source_event_id = se.id OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id));
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        RAISE NOTICE '00084 group D: nothing to settle';
    ELSIF n <> 5 THEN
        RAISE EXCEPTION '00084 group D: % events match, expected 5; resolve by hand', n;
    ELSE
        settled := settled || 'D';
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        SELECT household_id, 'SYSTEM', 'REPAIR_STUCK_SOURCE_EVENT', 'source_event', id,
               jsonb_build_object('group', 'D', 'processing_status', 'NEEDS_REVIEW'),
               jsonb_build_object('group', 'D', 'processing_status', 'IGNORED', 'migration', mig, 'ticket', '356')
        FROM source_event WHERE id = ANY(ids);
        UPDATE source_event SET processing_status = 'IGNORED' WHERE id = ANY(ids) AND processing_status = 'NEEDS_REVIEW';
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> n THEN RAISE EXCEPTION '00084 group D: % of % rows changed', changed, n; END IF;
    END IF;

    -- E: transfer proofs a manual unlink left with stale NEEDS_REVIEW proposals.
    SELECT array_agg(se.id) INTO ids FROM source_event se
    WHERE (scope IS NULL OR se.household_id = scope) AND se.received_at < cutoff
      AND se.source_type = 'TELEGRAM_IMAGE' AND se.processing_status = 'NEEDS_REVIEW'
      AND EXISTS (SELECT 1 FROM document d WHERE d.source_event_id = se.id AND d.document_type = 'TRANSFER_PROOF' AND d.status = 'EXTRACTED')
      AND EXISTS (SELECT 1 FROM audit_log a WHERE a.action = 'MANUAL_UNLINK_EVIDENCE' AND a.household_id = se.household_id
                  AND jsonb_typeof(a.after_json->'source_events') = 'array' AND (a.after_json->'source_events') ? se.id::text)
      AND EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id AND (p.proposal_status <> 'NEEDS_REVIEW' OR p.proposal_key !~ '^row-'))
      AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id = se.id
                      OR ri.document_id IN (SELECT d.id FROM document d WHERE d.source_event_id = se.id)
                      OR ri.proposal_id IN (SELECT p.id FROM transaction_proposal p WHERE p.source_event_id = se.id))
      AND NOT EXISTS (SELECT 1 FROM job j WHERE j.type = 'PROCESS_TRANSACTION_SCREENSHOT' AND j.status IN ('PENDING','RUNNING')
                      AND j.payload_json->>'document_id' IN (SELECT d.id::text FROM document d WHERE d.source_event_id = se.id));
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        RAISE NOTICE '00084 group E: nothing to settle';
    ELSIF n <> 2 THEN
        RAISE EXCEPTION '00084 group E: % events match, expected 2; resolve by hand', n;
    ELSE
        settled := settled || 'E';
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        SELECT se.household_id, 'SYSTEM', 'REPAIR_STUCK_SOURCE_EVENT', 'source_event', se.id,
               jsonb_build_object('group', 'E', 'processing_status', 'NEEDS_REVIEW', 'document_status', 'EXTRACTED'),
               jsonb_build_object('group', 'E', 'processing_status', 'PROCESSING', 'document_status', 'CLASSIFIED',
                                  'proposal_status', 'REJECTED', 'queued_job', 'PROCESS_TRANSACTION_SCREENSHOT', 'migration', mig, 'ticket', '356')
        FROM source_event se WHERE se.id = ANY(ids);
        UPDATE transaction_proposal
        SET proposal_status = 'REJECTED', proposal_key = 'superseded-' || proposal_key,
            metadata_json = metadata_json || jsonb_build_object('superseded_by_migration', mig)
        WHERE source_event_id = ANY(ids) AND proposal_status = 'NEEDS_REVIEW';
        UPDATE document SET status = 'CLASSIFIED', updated_at = now() WHERE source_event_id = ANY(ids) AND status = 'EXTRACTED';
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> n THEN RAISE EXCEPTION '00084 group E: % of % documents changed', changed, n; END IF;
        UPDATE source_event SET processing_status = 'PROCESSING' WHERE id = ANY(ids) AND processing_status = 'NEEDS_REVIEW';
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> n THEN RAISE EXCEPTION '00084 group E: % of % rows changed', changed, n; END IF;
        INSERT INTO job(type, payload_json, max_attempts)
        SELECT 'PROCESS_TRANSACTION_SCREENSHOT', jsonb_build_object('document_id', d.id), 5
        FROM document d WHERE d.source_event_id = ANY(ids);
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> n THEN RAISE EXCEPTION '00084 group E: % of % jobs queued', changed, n; END IF;
    END IF;

    -- All five groups or none. Group E is found through an audit row written by a
    -- one-off manual statement, not by any code, so a database whose signature
    -- differs must fail here instead of silently leaving that group stuck.
    IF settled <> '' AND length(settled) <> 5 THEN
        RAISE EXCEPTION '00084: groups % matched but the others matched nothing; this is not the inspected database, resolve by hand', settled;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Restores each event this migration settled, from the prior state held in its
-- audit_log row, and only while the event is still in the state Up left it in.
-- An event the household or a worker has moved on from is left alone. Group E is
-- restored only while its queued job has not started; once the reprocess has run
-- its outcome is real evidence and stays. Each rollback writes its own audit row
-- so a second Down is a no-op and a later Up can run again.
-- +goose StatementBegin
DO $$
DECLARE
    scope uuid := NULLIF(current_setting('richmod.repair_household', true), '')::uuid;
    rec record;
    restored boolean;
    mig CONSTANT text := '00084_settle_stuck_source_events';
BEGIN
    FOR rec IN
        SELECT a.entity_id AS id, a.household_id AS hh, a.before_json->>'group' AS grp, a.created_at AS at
        FROM audit_log a
        WHERE a.action = 'REPAIR_STUCK_SOURCE_EVENT' AND a.after_json->>'migration' = mig AND (scope IS NULL OR a.household_id = scope)
          AND NOT EXISTS (SELECT 1 FROM audit_log r WHERE r.action = 'ROLLBACK_REPAIR_STUCK_SOURCE_EVENT' AND r.entity_id = a.entity_id
                          AND r.after_json->>'migration' = mig AND r.created_at >= a.created_at)
        ORDER BY a.created_at
    LOOP
        restored := false;
        IF rec.grp IN ('A', 'C') THEN
            UPDATE source_event SET processing_status = 'NEEDS_REVIEW' WHERE id = rec.id AND processing_status = 'PROCESSED';
            restored := FOUND;
        ELSIF rec.grp = 'B' THEN
            -- Restore unless the household already dismissed the failed-source item;
            -- remove the item only when this migration wrote it.
            UPDATE source_event SET processing_status = 'NEEDS_REVIEW' WHERE id = rec.id AND processing_status = 'FAILED'
              AND NOT EXISTS (SELECT 1 FROM integration_action ia WHERE ia.household_id = rec.hh AND ia.dedupe_key = rec.id::text
                              AND ia.action_type = 'SOURCE_FAILED' AND ia.status <> 'OPEN');
            restored := FOUND;
            IF restored THEN
                DELETE FROM integration_action WHERE household_id = rec.hh AND dedupe_key = rec.id::text AND action_type = 'SOURCE_FAILED'
                  AND status = 'OPEN' AND metadata_json->>'repair_migration' = mig;
            END IF;
        ELSIF rec.grp = 'D' THEN
            UPDATE source_event SET processing_status = 'NEEDS_REVIEW' WHERE id = rec.id AND processing_status = 'IGNORED';
            restored := FOUND;
        ELSIF rec.grp = 'E' THEN
            IF EXISTS (SELECT 1 FROM source_event WHERE id = rec.id AND processing_status = 'PROCESSING')
               AND EXISTS (SELECT 1 FROM document d WHERE d.source_event_id = rec.id AND d.status = 'CLASSIFIED')
               AND NOT EXISTS (SELECT 1 FROM job j WHERE j.type = 'PROCESS_TRANSACTION_SCREENSHOT' AND j.status <> 'PENDING' AND j.created_at >= rec.at
                               AND j.payload_json->>'document_id' IN (SELECT d.id::text FROM document d WHERE d.source_event_id = rec.id))
               AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id = rec.id) THEN
                DELETE FROM job j WHERE j.type = 'PROCESS_TRANSACTION_SCREENSHOT' AND j.status = 'PENDING' AND j.created_at >= rec.at
                  AND j.payload_json->>'document_id' IN (SELECT d.id::text FROM document d WHERE d.source_event_id = rec.id);
                UPDATE transaction_proposal
                SET proposal_status = 'NEEDS_REVIEW', proposal_key = regexp_replace(proposal_key, '^superseded-', ''),
                    metadata_json = metadata_json - 'superseded_by_migration'
                WHERE source_event_id = rec.id AND metadata_json->>'superseded_by_migration' = mig;
                UPDATE document SET status = 'EXTRACTED', updated_at = now() WHERE source_event_id = rec.id AND status = 'CLASSIFIED';
                UPDATE source_event SET processing_status = 'NEEDS_REVIEW' WHERE id = rec.id;
                restored := true;
            END IF;
        END IF;
        IF restored THEN
            INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
            VALUES (rec.hh, 'SYSTEM', 'ROLLBACK_REPAIR_STUCK_SOURCE_EVENT', 'source_event', rec.id,
                    jsonb_build_object('group', rec.grp), jsonb_build_object('group', rec.grp, 'migration', mig, 'ticket', '356'));
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd
