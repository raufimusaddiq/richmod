-- +goose Up
CREATE TABLE review_request_recipient (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    review_request_id UUID NOT NULL REFERENCES review_request(id),
    telegram_chat_id BIGINT NOT NULL,
    telegram_message_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(review_request_id, telegram_chat_id),
    UNIQUE(telegram_chat_id, telegram_message_id)
);
CREATE INDEX review_request_recipient_lookup_idx ON review_request_recipient(telegram_chat_id, telegram_message_id);

-- +goose Down
DROP TABLE review_request_recipient;
