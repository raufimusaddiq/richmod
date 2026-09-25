-- +goose Up
-- UIR-02: a document, payslip, bank, or financial-email review now receives a
-- Telegram projection, so review_request.review_type must accept the same
-- reasons review_item already allows. Without this the projection insert fails
-- the review_request_review_type_check constraint (SQLSTATE 23514) and the
-- review never reaches Telegram.
ALTER TABLE review_request DROP CONSTRAINT review_request_review_type_check;
ALTER TABLE review_request ADD CONSTRAINT review_request_review_type_check CHECK (review_type IN (
    'UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE',
    'CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH',
    'DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION',
    'DOCUMENT_CLASSIFICATION','PAYSLIP_CONFIRMATION','MISSING_PAY_DATE',
    'SALARY_SOURCE_CONFIRMATION','UNKNOWN_BANK_TEMPLATE','INVOICE_PAYMENT_STATUS',
    'CYCLE_RESIDUAL_ALLOCATION','WEALTH_OBSERVATION_CONFIRMATION',
    'FINANCIAL_EMAIL_RESOLUTION','MISSING_TRANSACTION_DATE','TRANSACTION_FACTS_MISSING'
));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM review_request WHERE review_type IN (
        'DOCUMENT_CLASSIFICATION','PAYSLIP_CONFIRMATION','MISSING_PAY_DATE',
        'SALARY_SOURCE_CONFIRMATION','UNKNOWN_BANK_TEMPLATE','INVOICE_PAYMENT_STATUS'
    )) THEN
        RAISE EXCEPTION 'cannot roll back projected review request types while requests still use them';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE review_request DROP CONSTRAINT review_request_review_type_check;
ALTER TABLE review_request ADD CONSTRAINT review_request_review_type_check CHECK (review_type IN (
    'UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE',
    'CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH',
    'DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION',
    'CYCLE_RESIDUAL_ALLOCATION','WEALTH_OBSERVATION_CONFIRMATION',
    'FINANCIAL_EMAIL_RESOLUTION','MISSING_TRANSACTION_DATE','TRANSACTION_FACTS_MISSING'
));
