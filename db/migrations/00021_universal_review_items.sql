-- +goose Up
CREATE TABLE review_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    transaction_id UUID REFERENCES transaction(id),
    proposal_id UUID REFERENCES transaction_proposal(id),
    source_event_id UUID REFERENCES source_event(id),
    document_id UUID REFERENCES document(id),
    review_type TEXT NOT NULL CHECK (review_type IN (
        'UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY',
        'POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE',
        'RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE',
        'TRANSFER_CLASSIFICATION','MANUAL_CORRECTION','DOCUMENT_CLASSIFICATION',
        'PAYSLIP_CONFIRMATION','MISSING_PAY_DATE','SALARY_SOURCE_CONFIRMATION',
        'UNKNOWN_BANK_TEMPLATE','INVOICE_PAYMENT_STATUS'
    )),
    status TEXT NOT NULL CHECK (status IN ('PENDING_SEND','OPEN','RESOLVED','EXPIRED','CANCELLED')),
    preferred_user_id UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    CHECK (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL),
    CHECK ((status = 'RESOLVED') = (resolved_at IS NOT NULL))
);
CREATE INDEX review_item_household_status_idx ON review_item(household_id,status,created_at DESC);
CREATE INDEX review_item_source_event_idx ON review_item(source_event_id) WHERE source_event_id IS NOT NULL;
CREATE INDEX review_item_document_idx ON review_item(document_id) WHERE document_id IS NOT NULL;

ALTER TABLE review_request ADD COLUMN review_item_id UUID REFERENCES review_item(id);
INSERT INTO review_item(id,household_id,transaction_id,review_type,status,created_at,resolved_at)
SELECT id,household_id,transaction_id,review_type,status,created_at,resolved_at FROM review_request;
UPDATE review_request SET review_item_id=id;
CREATE UNIQUE INDEX review_request_review_item_unique ON review_request(review_item_id) WHERE review_item_id IS NOT NULL;

-- +goose Down
DROP INDEX review_request_review_item_unique;
ALTER TABLE review_request DROP COLUMN review_item_id;
DROP TABLE review_item;
