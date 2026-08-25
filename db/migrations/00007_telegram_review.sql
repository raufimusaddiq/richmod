-- +goose Up
CREATE TABLE review_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    transaction_id UUID NOT NULL REFERENCES transaction(id),
    review_type TEXT NOT NULL CHECK (review_type IN ('UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION')),
    telegram_chat_id BIGINT NOT NULL,
    telegram_message_id BIGINT,
    status TEXT NOT NULL CHECK (status IN ('PENDING_SEND','OPEN','RESOLVED','EXPIRED','CANCELLED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '7 days',
    resolved_at TIMESTAMPTZ,
    CHECK ((status = 'RESOLVED') = (resolved_at IS NOT NULL))
);
CREATE UNIQUE INDEX review_request_open_transaction_unique
    ON review_request (transaction_id) WHERE status IN ('PENDING_SEND','OPEN');
CREATE UNIQUE INDEX review_request_telegram_message_unique
    ON review_request (telegram_chat_id,telegram_message_id) WHERE telegram_message_id IS NOT NULL;

CREATE TABLE review_conversation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    review_request_id UUID NOT NULL UNIQUE REFERENCES review_request(id),
    state TEXT NOT NULL CHECK (state IN ('AWAITING_PURPOSE','AWAITING_CATEGORY','AWAITING_CONFIRMATION','RESOLVED')),
    context_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE review_conversation;
DROP TABLE review_request;
