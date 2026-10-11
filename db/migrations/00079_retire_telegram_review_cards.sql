-- +goose Up
-- A review_request is the Telegram projection of a canonical review_item; one
-- request can be delivered as one card per household recipient. When the
-- request leaves its active state, every delivered card must lose its buttons
-- so no recipient can tap a dead card. The database enqueues one
-- RETIRE_TELEGRAM_REVIEW_CARD job per delivered card in the same transaction
-- as the terminal transition; the worker re-checks the request and edits the
-- card. The trigger stays dumb: the closure wording is chosen by the worker
-- from the request status when the job runs.

-- The last text Telegram shows on each delivered card (written when the card is
-- bound and after each successful edit), so retirement can append a closure note
-- with editMessageText. Without it the worker only strips the keyboard.
ALTER TABLE review_request_recipient ADD COLUMN delivered_text TEXT;

-- The separate merchant-learning question ("remember this category?") sent after
-- a Telegram confirmation. It is its own message so retiring the review card
-- cannot remove the learning buttons; its callbacks bind through this id.
ALTER TABLE review_request_recipient ADD COLUMN merchant_learning_message_id BIGINT;
CREATE UNIQUE INDEX review_request_recipient_learning_message_unique
    ON review_request_recipient(telegram_chat_id, merchant_learning_message_id)
    WHERE merchant_learning_message_id IS NOT NULL;

-- At most one queued or running retirement per delivered card. A finished job
-- does not block a later one: an EXPIRED request renewed to OPEN and closed
-- again must be able to retire the same card a second time.
CREATE UNIQUE INDEX job_retire_review_card_active_unique
    ON job((payload_json->>'recipient_id'), (payload_json->>'message_id'))
    WHERE type = 'RETIRE_TELEGRAM_REVIEW_CARD' AND status IN ('PENDING','RUNNING');

-- +goose StatementBegin
CREATE FUNCTION enqueue_review_card_retirement(p_review_request_id UUID, p_recipient_id UUID) RETURNS INTEGER LANGUAGE plpgsql AS $$
DECLARE
    queued INTEGER;
BEGIN
    INSERT INTO job(type, payload_json)
    SELECT 'RETIRE_TELEGRAM_REVIEW_CARD',
           jsonb_build_object(
               'review_request_id', r.id::text,
               'recipient_id', rr.id::text,
               'chat_id', rr.telegram_chat_id,
               'message_id', rr.telegram_message_id,
               'status', r.status)
    FROM review_request r
    JOIN review_request_recipient rr ON rr.review_request_id = r.id
    WHERE r.id = p_review_request_id
      AND r.status IN ('RESOLVED','CANCELLED','EXPIRED')
      AND rr.telegram_message_id IS NOT NULL
      AND (p_recipient_id IS NULL OR rr.id = p_recipient_id)
    ON CONFLICT DO NOTHING;
    GET DIAGNOSTICS queued = ROW_COUNT;
    RETURN queued;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION review_request_retire_telegram_cards() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM enqueue_review_card_retirement(NEW.id, NULL);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_request_retire_telegram_cards
    AFTER UPDATE OF status ON review_request
    FOR EACH ROW
    WHEN (OLD.status IN ('PENDING_SEND','OPEN') AND NEW.status IN ('RESOLVED','CANCELLED','EXPIRED'))
    EXECUTE FUNCTION review_request_retire_telegram_cards();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_job_lane() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.lane := CASE
    WHEN NEW.type IN ('PROCESS_TELEGRAM_CALLBACK','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','PROCESS_TELEGRAM_REVIEW_TEXT','RETIRE_TELEGRAM_REVIEW_CARD') THEN 'INTERACTIVE'
    WHEN NEW.type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL','PROCESS_FINANCIAL_EMAIL','PROCESS_FINANCIAL_EMAIL_PREVIEW') THEN 'BACKGROUND'
    ELSE 'DEFAULT'
  END;
  RETURN NEW;
END $$;
-- +goose StatementEnd

-- Repair: bank-email processing used to settle source-bound review_items
-- (EMAIL_RECEIVED_AT_FALLBACK) without closing their Telegram projection, so a
-- request could stay OPEN with live buttons after its canonical item was
-- already terminal. That is not ambiguous: the canonical item decides. Close
-- such requests and their conversations; the trigger above then queues
-- retirement of every delivered card. Not reversed by Down (the item was
-- already terminal; reopening the projection would recreate the defect).
UPDATE review_conversation SET state='RESOLVED',updated_at=now()
WHERE state <> 'RESOLVED' AND review_request_id IN (
    SELECT r.id FROM review_request r JOIN review_item ri ON ri.id=r.review_item_id
    WHERE r.status IN ('PENDING_SEND','OPEN') AND ri.status NOT IN ('PENDING_SEND','OPEN'));
UPDATE review_request r
SET status=CASE WHEN ri.status='RESOLVED' THEN 'RESOLVED' ELSE 'CANCELLED' END,
    resolved_at=COALESCE(ri.resolved_at, now())
FROM review_item ri
WHERE ri.id=r.review_item_id AND r.status IN ('PENDING_SEND','OPEN') AND ri.status NOT IN ('PENDING_SEND','OPEN');

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_job_lane() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.lane := CASE
    WHEN NEW.type IN ('PROCESS_TELEGRAM_CALLBACK','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','PROCESS_TELEGRAM_REVIEW_TEXT') THEN 'INTERACTIVE'
    WHEN NEW.type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL','PROCESS_FINANCIAL_EMAIL','PROCESS_FINANCIAL_EMAIL_PREVIEW') THEN 'BACKGROUND'
    ELSE 'DEFAULT'
  END;
  RETURN NEW;
END $$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS review_request_retire_telegram_cards ON review_request;
DROP FUNCTION IF EXISTS review_request_retire_telegram_cards();
DROP FUNCTION IF EXISTS enqueue_review_card_retirement(UUID, UUID);
-- A worker without the handler cannot run queued retirements; close them
-- instead of leaving them to fail as an unsupported type.
UPDATE job SET status='FAILED',locked_at=NULL,locked_by=NULL,last_error='migration 00079 rolled back',finished_at=now(),updated_at=now()
WHERE type='RETIRE_TELEGRAM_REVIEW_CARD' AND status IN ('PENDING','RUNNING');
DROP INDEX IF EXISTS job_retire_review_card_active_unique;
DROP INDEX IF EXISTS review_request_recipient_learning_message_unique;
ALTER TABLE review_request_recipient DROP COLUMN IF EXISTS merchant_learning_message_id;
ALTER TABLE review_request_recipient DROP COLUMN IF EXISTS delivered_text;
