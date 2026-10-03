-- +goose Up
-- Follow-up to the CEU-02 review (PR #285). 00076 is already applied, so the fixes
-- are a new forward migration.
--
-- 1. telegram_message_binding.entity_id must point at a document of the SAME
--    household. The reply router trusts this table's (chat, message) key, so a
--    dangling or cross-tenant row would be a mis-binding, not a cosmetic defect.
--    The composite key makes the database refuse it for any writer.
DELETE FROM telegram_message_binding b
WHERE NOT EXISTS (SELECT 1 FROM document d WHERE d.id = b.entity_id AND d.household_id = b.household_id);
ALTER TABLE document ADD CONSTRAINT document_id_household_unique UNIQUE (id, household_id);
ALTER TABLE telegram_message_binding
  ADD CONSTRAINT telegram_message_binding_document_fk
  FOREIGN KEY (entity_id, household_id) REFERENCES document (id, household_id);

-- 2. "One notice per document" must not depend on disposable job rows, which are
--    pruned. A durable marker on the document owns it. Documents that already have
--    a queued or sent notice are marked so a retry never announces them twice.
ALTER TABLE document ADD COLUMN evidence_notice_at TIMESTAMPTZ;
UPDATE document d SET evidence_notice_at = now()
WHERE EXISTS (SELECT 1 FROM job j WHERE j.type = 'SEND_TELEGRAM_MESSAGE' AND j.payload_json->>'bind_document_id' = d.id::text);

-- +goose Down
ALTER TABLE document DROP COLUMN IF EXISTS evidence_notice_at;
ALTER TABLE telegram_message_binding DROP CONSTRAINT IF EXISTS telegram_message_binding_document_fk;
ALTER TABLE document DROP CONSTRAINT IF EXISTS document_id_household_unique;
