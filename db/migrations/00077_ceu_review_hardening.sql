-- +goose Up
DELETE FROM telegram_message_binding b
WHERE NOT EXISTS (SELECT 1 FROM document d WHERE d.id = b.entity_id AND d.household_id = b.household_id);
ALTER TABLE document ADD CONSTRAINT document_id_household_unique UNIQUE (id, household_id);
ALTER TABLE telegram_message_binding
  ADD CONSTRAINT telegram_message_binding_document_fk
  FOREIGN KEY (entity_id, household_id) REFERENCES document (id, household_id);

ALTER TABLE document ADD COLUMN evidence_notice_at TIMESTAMPTZ;
UPDATE document d SET evidence_notice_at = now()
WHERE EXISTS (SELECT 1 FROM job j WHERE j.type = 'SEND_TELEGRAM_MESSAGE' AND j.payload_json->>'bind_document_id' = d.id::text);

-- +goose Down
ALTER TABLE document DROP COLUMN IF EXISTS evidence_notice_at;
ALTER TABLE telegram_message_binding DROP CONSTRAINT IF EXISTS telegram_message_binding_document_fk;
ALTER TABLE document DROP CONSTRAINT IF EXISTS document_id_household_unique;
