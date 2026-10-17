-- +goose Up
-- Ticket #356: 15 source events show as NEEDS_REVIEW although no active review
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
--        stays EXTRACTED: document.status has no IGNORED value.
--
-- Two transfer-proof events (group E in the ticket) are deliberately NOT here.
-- Reprocessing them can write a second CONFIRMED transaction for a transfer that
-- is already recorded, which is money and not a status settlement; the runbook
-- settles them by hand with a before/after check.
--
-- No row id is named. Each group is found by the state signature the inspection
-- established among events received before 2026-10-07 (the batch inspected;
-- group A's cause is still live in code, so later events must neither be
-- touched nor block this deploy), and the migration refuses to run unless the
-- group has exactly its expected size (A 6, B 3, C 1, D 5) or is empty, so a
-- database that differs from the inspected one fails loudly instead of being
-- half-repaired. The four groups are all-or-nothing: if some match and others
-- match nothing (for instance because one was already settled by hand), the
-- whole migration raises. Events at or after the cutoff stay visible to the
-- detection query for their own follow-up. An empty database is a notice and a
-- no-op, so fresh and already-repaired databases migrate cleanly and re-running
-- is idempotent. The optional session setting richmod.repair_household narrows
-- every group to one household; it is unset in a normal migrate.
--
-- Every changed event gets one audit_log row (REPAIR_STUCK_SOURCE_EVENT) holding
-- the prior state, which Down uses to restore it. No transaction, evidence,
-- proposal, job or review row is created, changed or deleted.
--
-- Note on D: reviewdomain.ResolveDocumentReview writes document.status='IGNORED',
-- which document_status_check forbids; that is a separate defect tracked outside
-- this repair, so the migration does not depend on it.
-- +goose StatementBegin
DO $$
DECLARE
    scope uuid := NULLIF(current_setting('richmod.repair_household', true), '')::uuid;
    ids uuid[];
    n int;
    changed int;
    settled text := '';
    covered text := '';
    prior_counts jsonb;
    cutoff CONSTANT timestamptz := '2026-10-07 00:00:00+07';
    mig CONSTANT text := '00084_settle_stuck_source_events';
    stamp CONSTANT text := 'repair-00084';
BEGIN
    -- Events an earlier run of this migration settled and that were not rolled back
    -- (a Down skips an event the household has acted on since) count toward each
    -- group's expected size, so Down -> Up stays runnable.
    SELECT COALESCE(jsonb_object_agg(g, c), '{}'::jsonb) INTO prior_counts FROM (
        SELECT a.before_json->>'group' AS g, count(DISTINCT a.entity_id) AS c
        FROM audit_log a
        WHERE a.action = 'REPAIR_STUCK_SOURCE_EVENT' AND a.after_json->>'migration' = mig AND (scope IS NULL OR a.household_id = scope)
          AND NOT EXISTS (SELECT 1 FROM audit_log r WHERE r.action = 'ROLLBACK_REPAIR_STUCK_SOURCE_EVENT' AND r.entity_id = a.entity_id
                          AND r.after_json->>'migration' = mig AND r.created_at >= a.created_at)
        GROUP BY 1) t;

    -- A: chat replies parked as NEEDS_REVIEW.
    SELECT array_agg(se.id) INTO ids FROM source_event se
    WHERE (scope IS NULL OR se.household_id = scope) AND se.received_at < cutoff
      AND se.source_type = 'TELEGRAM_TEXT' AND se.processing_status = 'NEEDS_REVIEW' AND se.parser_name IS NULL
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_evidence te WHERE te.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM document d WHERE d.source_event_id = se.id);
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        IF COALESCE((prior_counts->>'A')::int, 0) > 0 THEN covered := covered || 'A'; END IF;
        RAISE NOTICE '00084 group A: nothing to settle';
    ELSIF n <> 6 - COALESCE((prior_counts->>'A')::int, 0) THEN
        RAISE EXCEPTION '00084 group A: % events match, expected %; resolve by hand', n, 6 - COALESCE((prior_counts->>'A')::int, 0);
    ELSE
        settled := settled || 'A';
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        SELECT household_id, 'SYSTEM', 'REPAIR_STUCK_SOURCE_EVENT', 'source_event', id,
               jsonb_build_object('group', 'A', 'processing_status', 'NEEDS_REVIEW', 'parser_name', parser_name),
               jsonb_build_object('group', 'A', 'processing_status', 'PROCESSED', 'parser_name', stamp, 'migration', mig, 'ticket', '356')
        FROM source_event WHERE id = ANY(ids);
        UPDATE source_event SET processing_status = 'PROCESSED', parser_name = stamp WHERE id = ANY(ids) AND processing_status = 'NEEDS_REVIEW';
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
        IF COALESCE((prior_counts->>'B')::int, 0) > 0 THEN covered := covered || 'B'; END IF;
        RAISE NOTICE '00084 group B: nothing to settle';
    ELSIF n <> 3 - COALESCE((prior_counts->>'B')::int, 0) THEN
        RAISE EXCEPTION '00084 group B: % events match, expected %; resolve by hand', n, 3 - COALESCE((prior_counts->>'B')::int, 0);
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
        -- Same item reviewdomain.RecordFailedSourceAction writes for a BANK_EMAIL / INVALID source;
        -- the text is reviewdomain.FailedSourceCopy("BANK_EMAIL", "INVALID"), pinned by a test.
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
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM transaction_proposal p WHERE p.source_event_id = se.id AND p.proposal_status = 'NEEDS_REVIEW')
      AND NOT EXISTS (SELECT 1 FROM transaction_evidence te JOIN transaction t ON t.id = te.transaction_id WHERE te.source_event_id = se.id AND t.status = 'NEEDS_REVIEW');
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        IF COALESCE((prior_counts->>'C')::int, 0) > 0 THEN covered := covered || 'C'; END IF;
        RAISE NOTICE '00084 group C: nothing to settle';
    ELSIF n <> 1 - COALESCE((prior_counts->>'C')::int, 0) THEN
        RAISE EXCEPTION '00084 group C: % events match, expected %; resolve by hand', n, 1 - COALESCE((prior_counts->>'C')::int, 0);
    ELSE
        settled := settled || 'C';
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        SELECT household_id, 'SYSTEM', 'REPAIR_STUCK_SOURCE_EVENT', 'source_event', id,
               jsonb_build_object('group', 'C', 'processing_status', 'NEEDS_REVIEW', 'parser_name', parser_name),
               jsonb_build_object('group', 'C', 'processing_status', 'PROCESSED', 'parser_name', stamp, 'migration', mig, 'ticket', '356')
        FROM source_event WHERE id = ANY(ids);
        UPDATE source_event SET processing_status = 'PROCESSED', parser_name = stamp WHERE id = ANY(ids) AND processing_status = 'NEEDS_REVIEW';
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
      AND NOT EXISTS (SELECT 1 FROM financial_email_observation f WHERE f.source_event_id = se.id)
      AND NOT EXISTS (SELECT 1 FROM wealth_observation w JOIN document d ON d.id = w.document_id
                      WHERE d.source_event_id = se.id OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id));
    n := COALESCE(cardinality(ids), 0);
    IF n = 0 THEN
        IF COALESCE((prior_counts->>'D')::int, 0) > 0 THEN covered := covered || 'D'; END IF;
        RAISE NOTICE '00084 group D: nothing to settle';
    ELSIF n <> 5 - COALESCE((prior_counts->>'D')::int, 0) THEN
        RAISE EXCEPTION '00084 group D: % events match, expected %; resolve by hand', n, 5 - COALESCE((prior_counts->>'D')::int, 0);
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

    -- All four groups or none: a database where only some groups match is not the
    -- inspected one (or was partly settled by hand) and must fail here.
    IF settled <> '' AND length(settled || covered) <> 4 THEN
        RAISE EXCEPTION '00084: groups % matched but the others matched nothing; this is not the inspected database, resolve by hand', settled;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Restores each event this migration settled, from the prior state held in its
-- audit_log row, and only while the event is still in the state Up left it in.
-- An event the household or a worker has moved on from is left alone. Each rollback writes its own audit row
-- so a second Down is a no-op and a later Up can run again.
-- +goose StatementBegin
DO $$
DECLARE
    scope uuid := NULLIF(current_setting('richmod.repair_household', true), '')::uuid;
    rec record;
    restored boolean;
    mig CONSTANT text := '00084_settle_stuck_source_events';
    stamp CONSTANT text := 'repair-00084';
BEGIN
    FOR rec IN
        SELECT a.entity_id AS id, a.household_id AS hh, a.before_json->>'group' AS grp, a.before_json->>'parser_name' AS before_parser, a.created_at AS at
        FROM audit_log a
        WHERE a.action = 'REPAIR_STUCK_SOURCE_EVENT' AND a.after_json->>'migration' = mig AND (scope IS NULL OR a.household_id = scope)
          AND NOT EXISTS (SELECT 1 FROM audit_log r WHERE r.action = 'ROLLBACK_REPAIR_STUCK_SOURCE_EVENT' AND r.entity_id = a.entity_id
                          AND r.after_json->>'migration' = mig AND r.created_at >= a.created_at)
        ORDER BY a.created_at
    LOOP
        restored := false;
        IF rec.grp IN ('A', 'C') THEN
            -- Only while the event still carries the stamp Up wrote: most worker paths
            -- that finish the event afterwards rewrite parser_name and must not be undone; the
            -- A and C selectors also require no review item, proposal or evidence, which the
            -- few paths that keep parser_name only write.
            UPDATE source_event SET processing_status = 'NEEDS_REVIEW', parser_name = rec.before_parser
            WHERE id = rec.id AND processing_status = 'PROCESSED' AND parser_name = stamp;
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
