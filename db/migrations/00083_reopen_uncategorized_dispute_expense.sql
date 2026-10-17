-- +goose Up
-- Ticket #353: a confirmed expense whose review was RESOLVED still showed as
-- unfinished in the /analytics current cycle (UNCATEGORIZED_EXPENSE block).
--
-- Cause: legacy data. Before commit 4e03512 the evidence-dispute confirm path
-- (CONFIRM_EVIDENCE_DISPUTE) could confirm an EXPENSE without a category. The
-- current code refuses that (ErrMissingCategory), so no new row can reach this
-- state through the review flow.
--
-- Scope is deliberately ONE transaction, addressed by id. It is not "every
-- uncategorized confirmed expense": a null category on a CONFIRMED expense is a
-- legitimate state for manual ledger entries (ledger/manual.go), so a sweep
-- would wrongly reopen valid records, and AGENTS.md forbids guessing a category.
-- The row is additionally guarded so the statement is idempotent and a no-op on
-- any database where the record is absent or already handled:
--   * still CONFIRMED, type EXPENSE, category_id IS NULL;
--   * has a RESOLVED review item with resolution_action CONFIRM_EVIDENCE_DISPUTE
--     (the legacy path signature);
--   * has no active review item.
-- Effect: status -> NEEDS_REVIEW (confirmed_at cleared, as the
-- transaction_confirmed_timestamp CHECK requires) plus one OPEN
-- AMBIGUOUS_CATEGORY review item, the same contract the live flow writes for a
-- single missing category, with preferred_user_id taken from
-- transaction.created_by_user_id exactly as the live reopen contract does
-- (reviewdomain/reopen.go). An audit_log row keeps the prior state so Down can
-- restore it exactly.
--
-- Completable from the Web Inbox (option "a" of the #354 review): the legacy
-- row still carries an ACTIVE transaction_evidence row with
-- evidence_link_dispute='true' (the dispute-confirm path superseded the ORIGINAL
-- evidence, never the dispute row). ConfirmTransactionReview refuses a Web
-- CONFIRM_REVIEW while such an active row exists
-- (ErrEvidenceDisputeConfirmationRequired -> 409), and the only resolver for it
-- is the Telegram card. That dispute is already decided (its review is RESOLVED),
-- so Up marks the dispute row link_status='SUPERSEDED', the same marker
-- ResolveEvidenceDispute uses for superseded evidence. confirm.go and
-- duplicate.go only count ACTIVE dispute rows, so Web confirm then works. No
-- Telegram review_request is created, so nothing is sent from a migration.
--
-- Target id: the id was read from the production database (read-only) while
-- investigating #353. On any database where that id does not exist, or no longer
-- matches the guards above, every statement below is a no-op (the guarded SELECT
-- finds no row and the block returns before writing anything).
-- +goose StatementBegin
DO $$
DECLARE
    target CONSTANT uuid := '6ae4522f-8507-4688-a03e-d6b652279870';
    prior_confirmed_at timestamptz;
    hh uuid;
BEGIN
    SELECT t.household_id, t.confirmed_at INTO hh, prior_confirmed_at
    FROM transaction t
    WHERE t.id = target AND t.status = 'CONFIRMED' AND t.type = 'EXPENSE' AND t.category_id IS NULL
      AND EXISTS (SELECT 1 FROM review_item ri WHERE ri.transaction_id = t.id AND ri.household_id = t.household_id
                  AND ri.status = 'RESOLVED' AND ri.resolution_action = 'CONFIRM_EVIDENCE_DISPUTE')
      AND NOT EXISTS (SELECT 1 FROM review_item ri WHERE ri.transaction_id = t.id AND ri.status IN ('OPEN','PENDING_SEND'))
    FOR UPDATE;
    IF hh IS NULL THEN
        RETURN;
    END IF;
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
        'proposedFacts', '{}'::jsonb,
        'missingFacts', jsonb_build_array('category'),
        'evidenceRefs', jsonb_build_array(jsonb_build_object('kind', 'transaction', 'id', target::text)),
        'decisionSource', 'DETERMINISTIC',
        'decisionPolicyVersion', 'v1',
        'decisionProvenance', jsonb_build_object('migration', '00083_reopen_uncategorized_dispute_expense', 'ticket', '353'),
        'whyNotAutoConfirm', 'an expense needs a category and none was recorded when it was confirmed',
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
-- Restores the prior CONFIRMED state only if the household has not acted since:
-- the transaction is still NEEDS_REVIEW with no category and the review item
-- written by Up is still OPEN. Otherwise nothing is touched.
-- +goose StatementBegin
DO $$
DECLARE
    target CONSTANT uuid := '6ae4522f-8507-4688-a03e-d6b652279870';
    prior_confirmed_at timestamptz;
    hh uuid;
    item uuid;
BEGIN
    SELECT t.household_id, ri.id INTO hh, item
    FROM transaction t JOIN review_item ri ON ri.transaction_id = t.id AND ri.household_id = t.household_id
    WHERE t.id = target AND t.status = 'NEEDS_REVIEW' AND t.category_id IS NULL
      AND ri.status = 'OPEN' AND ri.decision->'decisionProvenance'->>'migration' = '00083_reopen_uncategorized_dispute_expense'
    FOR UPDATE OF t;
    IF hh IS NULL THEN
        RETURN;
    END IF;
    SELECT (a.before_json->>'confirmed_at')::timestamptz INTO prior_confirmed_at
    FROM audit_log a
    WHERE a.entity_id = target AND a.action = 'REOPEN_LEGACY_UNCATEGORIZED'
    ORDER BY a.created_at DESC LIMIT 1;
    IF prior_confirmed_at IS NULL THEN
        RAISE EXCEPTION 'cannot restore transaction %: original confirmed_at not found in audit_log', target;
    END IF;
    UPDATE review_item SET status = 'CANCELLED', resolution_action = 'MIGRATION_ROLLBACK', updated_at = now() WHERE id = item;
    UPDATE transaction_evidence
    SET metadata_json = (metadata_json - 'link_status' - 'superseded_by_migration')
    WHERE transaction_id = target AND metadata_json->>'superseded_by_migration' = '00083_reopen_uncategorized_dispute_expense';
    UPDATE transaction SET status = 'CONFIRMED', confirmed_at = prior_confirmed_at, updated_at = now() WHERE id = target;
    INSERT INTO audit_log(household_id, actor_type, action, entity_type, entity_id, before_json, after_json)
    VALUES (hh, 'SYSTEM', 'ROLLBACK_REOPEN_LEGACY_UNCATEGORIZED', 'transaction', target,
            jsonb_build_object('status', 'NEEDS_REVIEW'), jsonb_build_object('status', 'CONFIRMED'));
END;
$$;
-- +goose StatementEnd
