-- +goose Up
CREATE INDEX review_conversation_reprocess_document_idx
  ON review_conversation ((context_json->'reprocess'->>'document_id'))
  WHERE context_json ? 'reprocess';

-- +goose Down
DROP INDEX review_conversation_reprocess_document_idx;
