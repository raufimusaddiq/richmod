-- +goose Up

CREATE TABLE wealth_observation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    document_id UUID NOT NULL UNIQUE REFERENCES document(id),
    resolved_wealth_account_id UUID REFERENCES wealth_account(id),
    institution TEXT NOT NULL,
    account_hint TEXT NOT NULL,
    observed_value_idr NUMERIC(20,0) NOT NULL CHECK (observed_value_idr >= 0),
    quantity NUMERIC(30,10) CHECK (quantity IS NULL OR quantity >= 0),
    unit TEXT,
    unit_price_idr NUMERIC(20,0) CHECK (unit_price_idr IS NULL OR unit_price_idr >= 0),
    observed_date DATE,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPLIED','DISMISSED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX wealth_observation_household_pending_idx ON wealth_observation(household_id, status, created_at DESC);

ALTER TABLE document DROP CONSTRAINT document_document_type_check;
ALTER TABLE document ADD CONSTRAINT document_document_type_check CHECK (document_type IN ('RECEIPT','PAYSLIP','BANK_TRANSACTION_SCREENSHOT','TRANSFER_PROOF','EWALLET_SCREENSHOT','BILL_OR_INVOICE','TRANSACTION_HISTORY_SCREENSHOT','WEALTH_OBSERVATION','OTHER_FINANCIAL_DOCUMENT','NON_FINANCIAL_OR_UNSUPPORTED'));

CREATE TABLE transfer_reconciliation_case (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_event_id UUID NOT NULL UNIQUE REFERENCES source_event(id),
    account_id UUID NOT NULL REFERENCES account(id),
    amount_idr NUMERIC(20,0) NOT NULL CHECK (amount_idr > 0),
    transaction_at TIMESTAMPTZ NOT NULL,
    description TEXT,
    proposed_purpose TEXT NOT NULL,
    proposed_wealth_account_id UUID REFERENCES wealth_account(id),
    candidate_transaction_ids UUID[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','RESOLVED','DISMISSED')),
    resolved_at TIMESTAMPTZ,
    resolved_by_user_id UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX transfer_reconciliation_case_household_idx ON transfer_reconciliation_case(household_id, status, created_at DESC);

ALTER TABLE review_item ADD COLUMN wealth_observation_id UUID REFERENCES wealth_observation(id);
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item DROP CONSTRAINT review_item_review_type_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_review_type_check CHECK (review_type IN ('UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION','DOCUMENT_CLASSIFICATION','PAYSLIP_CONFIRMATION','MISSING_PAY_DATE','SALARY_SOURCE_CONFIRMATION','UNKNOWN_BANK_TEMPLATE','INVOICE_PAYMENT_STATUS','CYCLE_RESIDUAL_ALLOCATION','WEALTH_OBSERVATION_CONFIRMATION'));
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
    (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND source_event_id IS NULL AND document_id IS NULL AND wealth_observation_id IS NULL) OR
    (wealth_observation_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND source_event_id IS NULL AND document_id IS NULL AND cycle_residual_case_id IS NULL) OR
    (cycle_residual_case_id IS NULL AND wealth_observation_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL))
);
CREATE UNIQUE INDEX review_item_open_wealth_observation_unique ON review_item(wealth_observation_id) WHERE wealth_observation_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN');
ALTER TABLE review_request DROP CONSTRAINT IF EXISTS review_request_review_type_check;
ALTER TABLE review_request ADD CONSTRAINT review_request_review_type_check CHECK (review_type IN ('UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION','CYCLE_RESIDUAL_ALLOCATION','WEALTH_OBSERVATION_CONFIRMATION'));

-- +goose Down

DELETE FROM review_request_recipient WHERE review_request_id IN (SELECT id FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE wealth_observation_id IS NOT NULL));
DELETE FROM review_request WHERE review_item_id IN (SELECT id FROM review_item WHERE wealth_observation_id IS NOT NULL);
DELETE FROM review_item WHERE wealth_observation_id IS NOT NULL;
DROP INDEX transfer_reconciliation_case_household_idx;
DROP TABLE transfer_reconciliation_case;
DROP INDEX review_item_open_wealth_observation_unique;
ALTER TABLE review_item DROP CONSTRAINT review_item_subject_check;
ALTER TABLE review_item DROP CONSTRAINT review_item_review_type_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_review_type_check CHECK (review_type IN ('UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION','DOCUMENT_CLASSIFICATION','PAYSLIP_CONFIRMATION','MISSING_PAY_DATE','SALARY_SOURCE_CONFIRMATION','UNKNOWN_BANK_TEMPLATE','INVOICE_PAYMENT_STATUS','CYCLE_RESIDUAL_ALLOCATION'));
ALTER TABLE review_item ADD CONSTRAINT review_item_subject_check CHECK (
    (cycle_residual_case_id IS NULL AND (transaction_id IS NOT NULL OR proposal_id IS NOT NULL OR source_event_id IS NOT NULL OR document_id IS NOT NULL)) OR
    (cycle_residual_case_id IS NOT NULL AND transaction_id IS NULL AND proposal_id IS NULL AND source_event_id IS NULL AND document_id IS NULL)
);
ALTER TABLE review_item DROP COLUMN wealth_observation_id;
DROP TABLE wealth_observation;
UPDATE document SET document_type='OTHER_FINANCIAL_DOCUMENT',status='NEEDS_REVIEW',updated_at=now() WHERE document_type='WEALTH_OBSERVATION';
ALTER TABLE document DROP CONSTRAINT document_document_type_check;
ALTER TABLE document ADD CONSTRAINT document_document_type_check CHECK (document_type IN ('RECEIPT','PAYSLIP','BANK_TRANSACTION_SCREENSHOT','TRANSFER_PROOF','EWALLET_SCREENSHOT','BILL_OR_INVOICE','TRANSACTION_HISTORY_SCREENSHOT','OTHER_FINANCIAL_DOCUMENT','NON_FINANCIAL_OR_UNSUPPORTED'));
ALTER TABLE review_request DROP CONSTRAINT review_request_review_type_check;
ALTER TABLE review_request ADD CONSTRAINT review_request_review_type_check CHECK (review_type IN ('UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION','CYCLE_RESIDUAL_ALLOCATION'));
