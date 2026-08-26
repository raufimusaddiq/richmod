-- +goose Up
CREATE TABLE document_page (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES document(id),
    source_event_id UUID NOT NULL REFERENCES source_event(id),
    attachment_id UUID NOT NULL REFERENCES attachment(id),
    page_index INTEGER NOT NULL CHECK (page_index >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(document_id, page_index),
    UNIQUE(document_id, source_event_id, attachment_id)
);
CREATE INDEX document_page_document_idx ON document_page(document_id, page_index);

-- +goose Down
DROP TABLE document_page;
