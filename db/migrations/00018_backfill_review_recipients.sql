-- +goose Up
INSERT INTO review_request_recipient(review_request_id,telegram_chat_id,telegram_message_id)
SELECT r.id,r.telegram_chat_id,r.telegram_message_id
FROM review_request r
WHERE r.telegram_chat_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM review_request_recipient rr
USING review_request r
WHERE rr.review_request_id=r.id AND rr.telegram_chat_id=r.telegram_chat_id;
