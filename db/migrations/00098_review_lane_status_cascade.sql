-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION review_item_close_request() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE review_request
       SET status = 'RESOLVED', resolved_at = COALESCE(resolved_at, now())
     WHERE review_item_id = NEW.id AND status IN ('PENDING_SEND', 'OPEN', 'EXPIRED');
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_item_close_request
    AFTER UPDATE OF status ON review_item
    FOR EACH ROW WHEN (NEW.status = 'RESOLVED' AND OLD.status IS DISTINCT FROM 'RESOLVED')
    EXECUTE FUNCTION review_item_close_request();

-- +goose StatementBegin
CREATE FUNCTION review_request_close_conversation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE review_conversation
       SET state = 'RESOLVED', last_message_at = now(), updated_at = now()
     WHERE review_request_id = NEW.id AND state NOT IN ('RESOLVED', 'AWAITING_MERCHANT_DECISION');
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_request_close_conversation
    AFTER UPDATE OF status ON review_request
    FOR EACH ROW WHEN (NEW.status IN ('RESOLVED', 'CANCELLED') AND OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION review_request_close_conversation();

UPDATE review_conversation c SET state = 'RESOLVED', last_message_at = now(), updated_at = now()
FROM review_request r
WHERE r.id = c.review_request_id AND r.status IN ('RESOLVED', 'CANCELLED') AND c.state NOT IN ('RESOLVED', 'AWAITING_MERCHANT_DECISION');

-- +goose Down
DROP TRIGGER review_request_close_conversation ON review_request;
DROP FUNCTION review_request_close_conversation();
DROP TRIGGER review_item_close_request ON review_item;
DROP FUNCTION review_item_close_request();
