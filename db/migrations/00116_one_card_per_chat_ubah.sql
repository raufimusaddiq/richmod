-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_job_lane() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.lane := CASE
    WHEN NEW.type IN ('PROCESS_TELEGRAM_CALLBACK','PROCESS_TELEGRAM_CARD_ANSWER','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','PROCESS_TELEGRAM_REVIEW_TEXT','RETIRE_TELEGRAM_REVIEW_CARD','EXPIRE_UBAH_BUTTON','COMPLETE_BANK_REVIEW') THEN 'INTERACTIVE'
    WHEN NEW.type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','GENERATE_CYCLE_RESIDUAL_REVIEW','PROJECT_TELEGRAM_REVIEW') THEN 'DETERMINISTIC'
    WHEN NEW.type IN ('PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL','PROCESS_FINANCIAL_EMAIL','PROCESS_FINANCIAL_EMAIL_PREVIEW') THEN 'BACKGROUND'
    ELSE 'DEFAULT'
  END;
  RETURN NEW;
END $$;
-- +goose StatementEnd

ALTER TABLE review_request_message ADD COLUMN delivered_markup JSONB;

ALTER TABLE review_item ADD COLUMN resolved_via TEXT CHECK (resolved_via IN ('TELEGRAM','WEB','SYSTEM'));

-- +goose StatementBegin
CREATE FUNCTION review_item_stamp_resolved_via() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.resolved_via := CASE
        WHEN NEW.resolved_by_user_id IS NULL THEN 'SYSTEM'
        WHEN current_setting('application_name', true) = 'richmod-api' THEN 'WEB'
        ELSE 'TELEGRAM'
    END;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_item_stamp_resolved_via
    BEFORE UPDATE OF status ON review_item
    FOR EACH ROW
    WHEN (OLD.status IN ('OPEN','PENDING_SEND') AND NEW.status = 'RESOLVED')
    EXECUTE FUNCTION review_item_stamp_resolved_via();

ALTER TABLE product_telemetry_event DROP CONSTRAINT product_telemetry_event_interaction_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_interaction_check
  CHECK (
    event_type <> 'INTERACTION'
    OR (
      action IN ('ANSWERED', 'UNMATCHED', 'AMBIGUOUS', 'CLOSED_CARD_REPLY', 'REPEATED_INPUT', 'STEP_SHOWN', 'FINISHED_ON_WEB', 'REPLY_REPAIRED', 'UBAH_REOPENED')
      AND (source_type IS NULL OR source_type IN ('TAP', 'REPLY', 'THREAD_REPLY', 'PLAIN', 'LABEL_TEXT'))
      AND (source_type IS NULL OR action = 'ANSWERED')
      AND (decision_source IS NULL
           OR (action = 'REPLY_REPAIRED' AND decision_source IN ('EMPTY', 'BROKEN_TEXT', 'INTERNAL_CODE', 'TELEGRAM_MECHANICS'))
           OR (action = 'ANSWERED' AND decision_source = 'ASSISTANT_CARD'))
      AND (action <> 'REPLY_REPAIRED' OR decision_source IS NOT NULL)
      AND screen IS NULL
      AND changed_fields = '{}' AND bounded_choices = 0
    )
  );

-- +goose StatementBegin
CREATE FUNCTION enqueue_review_position_refresh(p_chat BIGINT) RETURNS INTEGER LANGUAGE plpgsql AS $$
DECLARE
    queued INTEGER;
BEGIN
    INSERT INTO job(type, payload_json, run_after)
    SELECT 'EDIT_TELEGRAM_MESSAGE',
           jsonb_build_object('chat_id', rr.telegram_chat_id, 'message_id', rr.telegram_message_id,
                              'review_request_id', rr.review_request_id::text, 'refresh_position', true),
           now() + interval '3 seconds'
    FROM review_request_recipient rr JOIN review_request r ON r.id = rr.review_request_id
    WHERE rr.telegram_chat_id = p_chat AND rr.delivery_status = 'SENT' AND rr.telegram_message_id IS NOT NULL
      AND r.status IN ('PENDING_SEND','OPEN')
      AND review_card_live_count(p_chat) = 1
      AND NOT EXISTS (SELECT 1 FROM job j WHERE j.type = 'EDIT_TELEGRAM_MESSAGE' AND j.status = 'PENDING'
                      AND j.payload_json->>'refresh_position' = 'true' AND j.payload_json->>'chat_id' = p_chat::text);
    GET DIAGNOSTICS queued = ROW_COUNT;
    RETURN queued;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION release_review_cards(p_chat BIGINT) RETURNS INTEGER LANGUAGE plpgsql AS $$
DECLARE
    released INTEGER := 0;
    next_recipient RECORD;
BEGIN
    PERFORM lock_review_card_chat(p_chat);
    UPDATE review_request_recipient SET delivery_status='SKIPPED', queued_payload=NULL
    WHERE id IN (
        SELECT rr.id
        FROM review_request_recipient rr
        JOIN review_request r ON r.id = rr.review_request_id
        JOIN review_item ri ON ri.id = r.review_item_id
        WHERE rr.telegram_chat_id = p_chat AND rr.delivery_status = 'QUEUED'
          AND (r.status IN ('RESOLVED','CANCELLED') OR ri.status NOT IN ('PENDING_SEND','OPEN') OR rr.queued_payload IS NULL)
        FOR UPDATE OF rr SKIP LOCKED);
    WHILE review_card_live_count(p_chat) < 1 LOOP
        SELECT rr.id, rr.review_request_id, rr.queued_payload INTO next_recipient
        FROM review_request_recipient rr
        JOIN review_request r ON r.id = rr.review_request_id
        JOIN review_item ri ON ri.id = r.review_item_id
        WHERE rr.telegram_chat_id = p_chat AND rr.delivery_status = 'QUEUED' AND rr.queued_payload IS NOT NULL
          AND r.status IN ('PENDING_SEND','OPEN','EXPIRED') AND ri.status IN ('PENDING_SEND','OPEN')
        ORDER BY rr.created_at, rr.id
        LIMIT 1
        FOR UPDATE OF rr, r SKIP LOCKED;
        EXIT WHEN NOT FOUND;
        INSERT INTO job(type, payload_json, max_attempts) VALUES ('SEND_TELEGRAM_MESSAGE', next_recipient.queued_payload, 8);
        UPDATE review_request_recipient SET delivery_status='PENDING', queued_payload=NULL
        WHERE id = next_recipient.id;
        UPDATE review_request
        SET status = CASE WHEN status = 'EXPIRED' THEN 'OPEN' ELSE status END,
            expires_at = GREATEST(expires_at, now() + interval '7 days')
        WHERE id = next_recipient.review_request_id;
        released := released + 1;
    END LOOP;
    PERFORM enqueue_review_position_refresh(p_chat);
    RETURN released;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION admit_review_card(p_recipient_id UUID, p_payload JSONB) RETURNS TEXT LANGUAGE plpgsql AS $$
DECLARE
    chat BIGINT;
    current_status TEXT;
BEGIN
    SELECT telegram_chat_id INTO chat FROM review_request_recipient WHERE id = p_recipient_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'review card recipient % not found', p_recipient_id;
    END IF;
    PERFORM lock_review_card_chat(chat);
    SELECT delivery_status INTO current_status FROM review_request_recipient WHERE id = p_recipient_id FOR UPDATE;
    IF current_status IN ('QUEUED','SENT','EXPIRED') OR EXISTS (
        SELECT 1 FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.status IN ('PENDING','RUNNING')
          AND j.payload_json->>'review_request_id' = p_payload->>'review_request_id'
          AND j.payload_json->>'chat_id' = chat::text) THEN
        RETURN current_status;
    END IF;
    IF review_card_live_count(chat, p_recipient_id) < 1 AND NOT EXISTS (
        SELECT 1 FROM review_request_recipient rr
        JOIN review_request r ON r.id = rr.review_request_id
        JOIN review_item ri ON ri.id = r.review_item_id
        WHERE rr.telegram_chat_id = chat AND rr.delivery_status = 'QUEUED' AND rr.queued_payload IS NOT NULL
          AND r.status IN ('PENDING_SEND','OPEN','EXPIRED') AND ri.status IN ('PENDING_SEND','OPEN')) THEN
        INSERT INTO job(type, payload_json, max_attempts) VALUES ('SEND_TELEGRAM_MESSAGE', p_payload, 8);
        UPDATE review_request_recipient SET delivery_status='PENDING', queued_payload=NULL
        WHERE id = p_recipient_id;
        RETURN 'PENDING';
    END IF;
    UPDATE review_request_recipient SET delivery_status='QUEUED', queued_payload=p_payload WHERE id = p_recipient_id;
    PERFORM enqueue_review_position_refresh(chat);
    RETURN 'QUEUED';
END;
$$;
-- +goose StatementEnd

-- +goose Down
DELETE FROM job WHERE type='EXPIRE_UBAH_BUTTON' AND status='PENDING';
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_job_lane() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.lane := CASE
    WHEN NEW.type IN ('PROCESS_TELEGRAM_CALLBACK','PROCESS_TELEGRAM_CARD_ANSWER','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','PROCESS_TELEGRAM_REVIEW_TEXT','RETIRE_TELEGRAM_REVIEW_CARD','COMPLETE_BANK_REVIEW') THEN 'INTERACTIVE'
    WHEN NEW.type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','GENERATE_CYCLE_RESIDUAL_REVIEW','PROJECT_TELEGRAM_REVIEW') THEN 'DETERMINISTIC'
    WHEN NEW.type IN ('PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL','PROCESS_FINANCIAL_EMAIL','PROCESS_FINANCIAL_EMAIL_PREVIEW') THEN 'BACKGROUND'
    ELSE 'DEFAULT'
  END;
  RETURN NEW;
END $$;
-- +goose StatementEnd

DELETE FROM product_telemetry_event WHERE event_type='INTERACTION' AND action='UBAH_REOPENED';
ALTER TABLE product_telemetry_event DROP CONSTRAINT product_telemetry_event_interaction_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_interaction_check
  CHECK (
    event_type <> 'INTERACTION'
    OR (
      action IN ('ANSWERED', 'UNMATCHED', 'AMBIGUOUS', 'CLOSED_CARD_REPLY', 'REPEATED_INPUT', 'STEP_SHOWN', 'FINISHED_ON_WEB', 'REPLY_REPAIRED')
      AND (source_type IS NULL OR source_type IN ('TAP', 'REPLY', 'THREAD_REPLY', 'PLAIN', 'LABEL_TEXT'))
      AND (source_type IS NULL OR action = 'ANSWERED')
      AND (decision_source IS NULL
           OR (action = 'REPLY_REPAIRED' AND decision_source IN ('EMPTY', 'BROKEN_TEXT', 'INTERNAL_CODE', 'TELEGRAM_MECHANICS'))
           OR (action = 'ANSWERED' AND decision_source = 'ASSISTANT_CARD'))
      AND (action <> 'REPLY_REPAIRED' OR decision_source IS NOT NULL)
      AND screen IS NULL
      AND changed_fields = '{}' AND bounded_choices = 0
    )
  );
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION admit_review_card(p_recipient_id UUID, p_payload JSONB) RETURNS TEXT LANGUAGE plpgsql AS $$
DECLARE
    chat BIGINT;
    current_status TEXT;
BEGIN
    SELECT telegram_chat_id INTO chat FROM review_request_recipient WHERE id = p_recipient_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'review card recipient % not found', p_recipient_id;
    END IF;
    PERFORM lock_review_card_chat(chat);
    SELECT delivery_status INTO current_status FROM review_request_recipient WHERE id = p_recipient_id FOR UPDATE;
    IF current_status IN ('QUEUED','SENT','EXPIRED') OR EXISTS (
        SELECT 1 FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.status IN ('PENDING','RUNNING')
          AND j.payload_json->>'review_request_id' = p_payload->>'review_request_id'
          AND j.payload_json->>'chat_id' = chat::text) THEN
        RETURN current_status;
    END IF;
    IF review_card_live_count(chat, p_recipient_id) < 3 AND NOT EXISTS (
        SELECT 1 FROM review_request_recipient rr
        JOIN review_request r ON r.id = rr.review_request_id
        JOIN review_item ri ON ri.id = r.review_item_id
        WHERE rr.telegram_chat_id = chat AND rr.delivery_status = 'QUEUED' AND rr.queued_payload IS NOT NULL
          AND r.status IN ('PENDING_SEND','OPEN','EXPIRED') AND ri.status IN ('PENDING_SEND','OPEN')) THEN
        INSERT INTO job(type, payload_json, max_attempts) VALUES ('SEND_TELEGRAM_MESSAGE', p_payload, 8);
        UPDATE review_request_recipient SET delivery_status='PENDING', queued_payload=NULL
        WHERE id = p_recipient_id;
        RETURN 'PENDING';
    END IF;
    UPDATE review_request_recipient SET delivery_status='QUEUED', queued_payload=p_payload WHERE id = p_recipient_id;
    RETURN 'QUEUED';
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION release_review_cards(p_chat BIGINT) RETURNS INTEGER LANGUAGE plpgsql AS $$
DECLARE
    released INTEGER := 0;
    next_recipient RECORD;
BEGIN
    PERFORM lock_review_card_chat(p_chat);
    UPDATE review_request_recipient SET delivery_status='SKIPPED', queued_payload=NULL
    WHERE id IN (
        SELECT rr.id
        FROM review_request_recipient rr
        JOIN review_request r ON r.id = rr.review_request_id
        JOIN review_item ri ON ri.id = r.review_item_id
        WHERE rr.telegram_chat_id = p_chat AND rr.delivery_status = 'QUEUED'
          AND (r.status IN ('RESOLVED','CANCELLED') OR ri.status NOT IN ('PENDING_SEND','OPEN') OR rr.queued_payload IS NULL)
        FOR UPDATE OF rr SKIP LOCKED);
    WHILE review_card_live_count(p_chat) < 3 LOOP
        SELECT rr.id, rr.review_request_id, rr.queued_payload INTO next_recipient
        FROM review_request_recipient rr
        JOIN review_request r ON r.id = rr.review_request_id
        JOIN review_item ri ON ri.id = r.review_item_id
        WHERE rr.telegram_chat_id = p_chat AND rr.delivery_status = 'QUEUED' AND rr.queued_payload IS NOT NULL
          AND r.status IN ('PENDING_SEND','OPEN','EXPIRED') AND ri.status IN ('PENDING_SEND','OPEN')
        ORDER BY rr.created_at, rr.id
        LIMIT 1
        FOR UPDATE OF rr, r SKIP LOCKED;
        EXIT WHEN NOT FOUND;
        INSERT INTO job(type, payload_json, max_attempts) VALUES ('SEND_TELEGRAM_MESSAGE', next_recipient.queued_payload, 8);
        UPDATE review_request_recipient SET delivery_status='PENDING', queued_payload=NULL
        WHERE id = next_recipient.id;
        UPDATE review_request
        SET status = CASE WHEN status = 'EXPIRED' THEN 'OPEN' ELSE status END,
            expires_at = GREATEST(expires_at, now() + interval '7 days')
        WHERE id = next_recipient.review_request_id;
        released := released + 1;
    END LOOP;
    RETURN released;
END;
$$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS enqueue_review_position_refresh(BIGINT);
DROP TRIGGER IF EXISTS review_item_stamp_resolved_via ON review_item;
DROP FUNCTION IF EXISTS review_item_stamp_resolved_via();
ALTER TABLE review_item DROP COLUMN IF EXISTS resolved_via;
ALTER TABLE review_request_message DROP COLUMN IF EXISTS delivered_markup;
