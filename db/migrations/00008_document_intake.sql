-- +goose Up
DROP INDEX source_event_payload_unique;
CREATE INDEX source_event_payload_hash_idx ON source_event (household_id,source_type,payload_hash);

CREATE TABLE attachment (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    content_hash BYTEA NOT NULL,
    media_type TEXT NOT NULL CHECK (media_type IN ('image/jpeg','image/png')),
    byte_size BIGINT NOT NULL CHECK (byte_size > 0 AND byte_size <= 10485760),
    width INTEGER NOT NULL CHECK (width > 0 AND width <= 10000),
    height INTEGER NOT NULL CHECK (height > 0 AND height <= 10000),
    storage_ref TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id,content_hash)
);

CREATE TABLE document (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_event_id UUID NOT NULL UNIQUE REFERENCES source_event(id),
    attachment_id UUID NOT NULL REFERENCES attachment(id),
    document_type TEXT CHECK (document_type IN ('RECEIPT','PAYSLIP','BANK_TRANSACTION_SCREENSHOT','TRANSFER_PROOF','EWALLET_SCREENSHOT','BILL_OR_INVOICE','TRANSACTION_HISTORY_SCREENSHOT','OTHER_FINANCIAL_DOCUMENT','NON_FINANCIAL_OR_UNSUPPORTED')),
    status TEXT NOT NULL CHECK (status IN ('RECEIVED','CLASSIFIED','EXTRACTED','NEEDS_REVIEW','FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX document_household_created_idx ON document (household_id,created_at DESC);

CREATE TABLE document_extraction (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES document(id),
    stage TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    output_json JSONB NOT NULL,
    confidence NUMERIC(5,4) CHECK (confidence >= 0 AND confidence <= 1),
    gateway_model TEXT,
    validated BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id,stage,schema_version)
);

-- +goose Down
DROP TABLE document_extraction;
DROP TABLE document;
DROP TABLE attachment;
-- The non-unique evidence hash index is intentionally retained. Restoring the
-- former uniqueness constraint would require deleting valid duplicate evidence.
DROP INDEX source_event_payload_hash_idx;
CREATE INDEX source_event_payload_unique ON source_event (household_id,source_type,payload_hash);
