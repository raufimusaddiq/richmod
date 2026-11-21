-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
    scope uuid := NULLIF(current_setting('richmod.repair_household', true), '')::uuid;
    matches uuid[];
    target uuid;
    prior_confirmed_at timestamptz;
    hh uuid;
BEGIN
    SELECT array_agg(t.id) INTO matches
    FROM transaction t
    WHERE (scope IS NULL OR t.household_id = scope)
      AND t.status = 'CONFIRMED' AND t.type = 'EXPENSE' AND t.category_id IS NULL
      AND EXISTS (SELECT 1 FROM review_item ri WHERE ri.transaction_id = t.id AND ri.household_id = t.household_id
                  AND ri.status = 'RESOLVED' AND ri.resolution_action = 'CONFIRM_EVIDENCE_DISPUTE')
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.transaction_id = t.id AND ri.status IN ('OPEN','PENDING_SEND'));
    IF COALESCE(cardinality(matches), 0) = 0 THEN
        RAISE NOTICE '00083: no legacy uncategorized dispute expense found; nothing to reopen';
        RETURN;
    END IF;
    IF cardinality(matches) > 1 THEN
        RAISE EXCEPTION '00083: % transactions match the legacy uncategorized dispute signature, expected at most 1; resolve by hand', cardinality(matches);
    END IF;
    target := matches[1];
    SELECT t.household_id, t.confirmed_at INTO hh, prior_confirmed_at FROM transaction t WHERE t.id = target FOR UPDATE;
    UPDATE transaction SET status = 'NEEDS_REVIEW', confirmed_at = NULL, updated_at = now() WHERE id = target;
    UPDATE transaction_evidence
    SET metadata_json = metadata_json || jsonb_build_object('link_status', 'SUPERSEDED', 'superseded_by_migration', '00083_reopen_uncategorized_dispute_expense')
    WHERE transaction_id = target AND metadata_json->>'evidence_link_dispute' = 'true'
      AND COALESCE(metadata_json->>'link_status', 'ACTIVE') = 'ACTIVE';
    INSERT INTO review_item(household_id, transaction_id, review_type, status, decision, preferred_user_id)
    SELECT hh, target, 'AMBIGUOUS_CATEGORY', 'OPEN', jsonb_build_object(
        'version', 1,
        'subject', jsonb_build_object('type', 'transaction', 'id', target::text),
        'reasonCode', 'AMBIGUOUS_CATEGORY',
        'decisionClass', 'EVIDENCE_GAP',
        'knownFacts', '{}'::jsonb,
        'missingFacts', jsonb_build_array('category'),
        'evidenceRefs', jsonb_build_array(jsonb_build_object('kind', 'transaction', 'id', target::text)),
        'decisionSource', 'DETERMINISTIC',
        'decisionPolicyVersion', '',
        'decisionProvenance', jsonb_build_object('migration', '00083_reopen_uncategorized_dispute_expense', 'ticket', '353'),
        'whyNotAutoConfirm', 'the category is not supported strongly enough to confirm',
        'allowedActions', jsonb_build_array('CONFIRM_REVIEW', 'IGNORE'),
        'interactionMode', 'SINGLE_FIELD'), t.created_by_user_id
    FROM transaction t WHERE t.id = target;
    INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
    VALUES (hh, 'SYSTEM', 'REOPEN_LEGACY_UNCATEGORIZED', 'transaction', target,
            jsonb_build_object('status', 'CONFIRMED', 'confirmed_at', prior_confirmed_at),
            jsonb_build_object('status', 'NEEDS_REVIEW', 'migration', '00083_reopen_uncategorized_dispute_expense', 'ticket', '353'));
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE
    rec record;
    prior_confirmed_at timestamptz;
BEGIN
    FOR rec IN
        SELECT t.id AS target, t.household_id AS hh, ri.id AS item
        FROM transaction t JOIN review_item ri ON ri.transaction_id = t.id AND ri.household_id = t.household_id
        WHERE t.status = 'NEEDS_REVIEW' AND t.category_id IS NULL
          AND ri.status = 'OPEN' AND ri.decision->'decisionProvenance'->>'migration' = '00083_reopen_uncategorized_dispute_expense'
        FOR UPDATE OF t
    LOOP
        SELECT (a.before_json->>'confirmed_at')::timestamptz INTO prior_confirmed_at
        FROM audit_log a
        WHERE a.entity_id = rec.target AND a.action = 'REOPEN_LEGACY_UNCATEGORIZED'
        ORDER BY a.created_at DESC LIMIT 1;
        IF prior_confirmed_at IS NULL THEN
            RAISE EXCEPTION 'cannot restore transaction %: original confirmed_at not found in audit_log', rec.target;
        END IF;
        UPDATE review_item SET status = 'CANCELLED', resolution_action = 'MIGRATION_ROLLBACK', updated_at = now() WHERE id = rec.item;
        UPDATE transaction_evidence SET metadata_json = metadata_json - 'link_status'
        WHERE transaction_id = rec.target AND metadata_json->>'superseded_by_migration' = '00083_reopen_uncategorized_dispute_expense';
        UPDATE transaction SET status = 'CONFIRMED', confirmed_at = prior_confirmed_at, updated_at = now() WHERE id = rec.target;
        INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
        VALUES (rec.hh, 'SYSTEM', 'ROLLBACK_REOPEN_LEGACY_UNCATEGORIZED', 'transaction', rec.target,
                jsonb_build_object('status', 'NEEDS_REVIEW'), jsonb_build_object('status', 'CONFIRMED'));
    END LOOP;
    UPDATE transaction_evidence SET metadata_json = metadata_json - 'superseded_by_migration'
    WHERE metadata_json->>'superseded_by_migration' = '00083_reopen_uncategorized_dispute_expense';
END;
$$;
-- +goose StatementEnd
