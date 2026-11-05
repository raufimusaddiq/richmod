-- +goose Up
-- Telegram review card delivery, binding and hygiene (ADR-046 amendment
-- 2026-11-05).
--
-- 1. review_request_recipient.delivery_status records where each card is:
--    QUEUED (held back by the per-chat pacing limit, its rendered send payload
--    kept in queued_payload), PENDING (a send job exists), SENT (bound to a
--    message), EXPIRED (this copy's own seven-day window passed and its buttons
--    were removed), FAILED (the send job gave up; delivery_error says why), or
--    SKIPPED (the request closed or the recipient lost access before it was
--    sent). card_expires_at is the copy's own window, which starts when
--    Telegram delivers the copy (its first bind), so a copy that waited in the
--    pacing queue or behind send retries gets its full window. The canonical review_item is never changed by any of these.
ALTER TABLE review_request_recipient
    ADD COLUMN delivery_status TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (delivery_status IN ('QUEUED','PENDING','SENT','EXPIRED','FAILED','SKIPPED')),
    ADD COLUMN queued_payload JSONB,
    ADD COLUMN card_expires_at TIMESTAMPTZ,
    ADD COLUMN delivery_error TEXT,
    ADD COLUMN delivery_failed_at TIMESTAMPTZ;

UPDATE review_request_recipient rr SET delivery_status='SENT',card_expires_at=r.expires_at
FROM review_request r WHERE r.id=rr.review_request_id AND rr.telegram_message_id IS NOT NULL;
-- An undelivered card whose send jobs all failed is a failed delivery, not a
-- pending one.
UPDATE review_request_recipient rr
SET delivery_status='FAILED',delivery_failed_at=now(),delivery_error='send job failed before migration 00094'
WHERE rr.telegram_message_id IS NULL
  AND EXISTS (SELECT 1 FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.status='FAILED'
              AND j.payload_json->>'review_request_id'=rr.review_request_id::text
              AND j.payload_json->>'chat_id'=rr.telegram_chat_id::text)
  AND NOT EXISTS (SELECT 1 FROM job j WHERE j.type='SEND_TELEGRAM_MESSAGE' AND j.status IN ('PENDING','RUNNING','SUCCEEDED')
              AND j.payload_json->>'review_request_id'=rr.review_request_id::text
              AND j.payload_json->>'chat_id'=rr.telegram_chat_id::text);

CREATE INDEX review_request_recipient_queued_idx
    ON review_request_recipient(telegram_chat_id, created_at)
    WHERE delivery_status = 'QUEUED';
CREATE INDEX review_request_recipient_live_chat_idx
    ON review_request_recipient(telegram_chat_id)
    WHERE delivery_status IN ('PENDING','SENT');

-- 2. Every Telegram message that belongs to a review, per recipient. The
-- recipient row keeps the one current card (the message taps and replies bind
-- to); an older message that a follow-up replaced stays here with
-- superseded_at set so its buttons are removed and it is retired on close.
CREATE TABLE review_request_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    review_request_id UUID NOT NULL REFERENCES review_request(id),
    recipient_id UUID NOT NULL REFERENCES review_request_recipient(id),
    telegram_chat_id BIGINT NOT NULL,
    telegram_message_id BIGINT NOT NULL,
    delivered_text TEXT,
    superseded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (telegram_chat_id, telegram_message_id)
);
CREATE INDEX review_request_message_request_idx ON review_request_message(review_request_id);

INSERT INTO review_request_message(household_id,review_request_id,recipient_id,telegram_chat_id,telegram_message_id,delivered_text,created_at)
SELECT r.household_id,r.id,rr.id,rr.telegram_chat_id,rr.telegram_message_id,rr.delivered_text,rr.created_at
FROM review_request_recipient rr JOIN review_request r ON r.id=rr.review_request_id
WHERE rr.telegram_message_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- 3. The message id of every successful sendMessage, recorded right after
-- Telegram accepts it and before any binding, keyed by the job. A retry of the
-- same job binds this message instead of sending a second card.
CREATE TABLE telegram_outbound_delivery (
    job_id UUID PRIMARY KEY REFERENCES job(id) ON DELETE CASCADE,
    household_id UUID REFERENCES household(id),
    telegram_chat_id BIGINT NOT NULL,
    telegram_message_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 4. Retirement covers every message of the request, current and superseded.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enqueue_review_card_retirement(p_review_request_id UUID, p_recipient_id UUID) RETURNS INTEGER LANGUAGE plpgsql AS $$
DECLARE
    queued INTEGER;
BEGIN
    INSERT INTO job(type, payload_json)
    SELECT 'RETIRE_TELEGRAM_REVIEW_CARD',
           jsonb_build_object(
               'review_request_id', r.id::text,
               'recipient_id', m.recipient_id::text,
               'chat_id', m.telegram_chat_id,
               'message_id', m.telegram_message_id,
               'status', r.status)
    FROM review_request r
    JOIN (
        SELECT rm.review_request_id, rm.recipient_id, rm.telegram_chat_id, rm.telegram_message_id
        FROM review_request_message rm
        UNION
        SELECT rr.review_request_id, rr.id, rr.telegram_chat_id, rr.telegram_message_id
        FROM review_request_recipient rr WHERE rr.telegram_message_id IS NOT NULL
    ) m ON m.review_request_id = r.id
    WHERE r.id = p_review_request_id
      AND r.status IN ('RESOLVED','CANCELLED','EXPIRED')
      AND (p_recipient_id IS NULL OR m.recipient_id = p_recipient_id)
    ON CONFLICT DO NOTHING;
    GET DIAGNOSTICS queued = ROW_COUNT;
    RETURN queued;
END;
$$;
-- +goose StatementEnd

-- 5. Pacing: at most three live review cards per chat. A card past the limit
-- waits as QUEUED and is released oldest-first when a live card closes,
-- expires, fails, or is skipped.
--
-- Lock order. Every path that changes pacing state takes the chat's
-- transaction-scoped advisory lock before it waits for any recipient row, and
-- a release, which runs while other transactions may hold request rows, claims
-- every row it touches with SKIP LOCKED, so it never waits while holding the
-- chat lock. A close (or projection expiry) holds its request row, then takes
-- its chats' advisory locks in chat order, then touches recipients. A request
-- skipped by a release because it is being closed is released by that close's
-- own trigger.
-- +goose StatementBegin
CREATE FUNCTION lock_review_card_chat(p_chat BIGINT) RETURNS VOID LANGUAGE sql AS $$
    SELECT pg_advisory_xact_lock(hashtextextended('review_card_pacing:' || p_chat::text, 0))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION review_card_live_count(p_chat BIGINT, p_except UUID DEFAULT NULL) RETURNS INTEGER LANGUAGE sql STABLE AS $$
    SELECT count(*)::int
    FROM review_request_recipient rr JOIN review_request r ON r.id = rr.review_request_id
    WHERE rr.telegram_chat_id = p_chat
      AND rr.delivery_status IN ('PENDING','SENT')
      AND r.status IN ('PENDING_SEND','OPEN')
      AND (p_except IS NULL OR rr.id <> p_except)
$$;
-- +goose StatementEnd

-- A queued copy is releasable while its review_item is open, even when the
-- request's projection expired: the other copies expired (they stay EXPIRED,
-- retired and uncounted), this copy has not been shown yet. Releasing it
-- reopens the request and starts this copy's own window.
-- +goose StatementBegin
CREATE FUNCTION release_review_cards(p_chat BIGINT) RETURNS INTEGER LANGUAGE plpgsql AS $$
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

-- +goose StatementBegin
CREATE FUNCTION admit_review_card(p_recipient_id UUID, p_payload JSONB) RETURNS TEXT LANGUAGE plpgsql AS $$
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
    -- A card that was already admitted (a re-run of the same projection) is not
    -- admitted twice.
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

-- One copy's window passed: it becomes EXPIRED, every message of it loses its
-- buttons (the note says the review is still in the Review Inbox), and its slot
-- is released. The request closes as EXPIRED only when no copy is still live
-- or waiting. Returns false when the copy is not due or is being changed by
-- another transaction (the next sweep retries it).
-- +goose StatementBegin
CREATE FUNCTION expire_review_card_recipient(p_recipient_id UUID) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
DECLARE
    due RECORD;
    request UUID;
    chat BIGINT;
BEGIN
    -- Chat locks first, in chat order, for every chat of the request (the same
    -- set its close trigger takes), then the rows, without waiting.
    SELECT review_request_id INTO request FROM review_request_recipient WHERE id = p_recipient_id;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;
    FOR chat IN SELECT DISTINCT telegram_chat_id FROM review_request_recipient WHERE review_request_id = request ORDER BY 1 LOOP
        PERFORM lock_review_card_chat(chat);
    END LOOP;
    SELECT rr.id, rr.review_request_id, rr.telegram_chat_id INTO due
    FROM review_request_recipient rr JOIN review_request r ON r.id = rr.review_request_id
    WHERE rr.id = p_recipient_id AND rr.delivery_status = 'SENT'
      AND r.status IN ('PENDING_SEND','OPEN')
      AND COALESCE(rr.card_expires_at, r.expires_at) <= now()
    FOR UPDATE OF r, rr SKIP LOCKED;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM review_request_recipient x
                   WHERE x.review_request_id = due.review_request_id AND x.id <> due.id
                     AND x.delivery_status IN ('QUEUED','PENDING','SENT')) THEN
        -- The last copy: the projection expires; the request triggers retire
        -- every message and release the chats.
        UPDATE review_request SET status='EXPIRED' WHERE id = due.review_request_id;
        RETURN TRUE;
    END IF;
    UPDATE review_request_recipient SET delivery_status='EXPIRED' WHERE id = due.id;
    INSERT INTO job(type, payload_json)
    SELECT 'RETIRE_TELEGRAM_REVIEW_CARD',
           jsonb_build_object('review_request_id', due.review_request_id::text, 'recipient_id', due.id::text,
                              'chat_id', m.telegram_chat_id, 'message_id', m.telegram_message_id, 'status', 'EXPIRED')
    FROM (SELECT rm.telegram_chat_id, rm.telegram_message_id FROM review_request_message rm WHERE rm.recipient_id = due.id
          UNION
          SELECT rr.telegram_chat_id, rr.telegram_message_id FROM review_request_recipient rr WHERE rr.id = due.id AND rr.telegram_message_id IS NOT NULL) m
    ON CONFLICT DO NOTHING;
    PERFORM release_review_cards(due.telegram_chat_id);
    RETURN TRUE;
END;
$$;
-- +goose StatementEnd

-- A closed or expired request frees its slots in every chat it was delivered
-- to. The chats' advisory locks are taken first, in chat order, before any
-- recipient row is touched. Live copies of an expired projection become
-- EXPIRED (retired, uncounted); queued copies of a resolved or cancelled
-- review are dropped; a queued copy of an expired projection stays queued.
-- +goose StatementBegin
CREATE FUNCTION review_request_release_paced_cards() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    chat BIGINT;
BEGIN
    FOR chat IN SELECT DISTINCT telegram_chat_id FROM review_request_recipient WHERE review_request_id = NEW.id ORDER BY 1 LOOP
        PERFORM lock_review_card_chat(chat);
    END LOOP;
    IF NEW.status = 'EXPIRED' THEN
        UPDATE review_request_recipient SET delivery_status='EXPIRED'
        WHERE review_request_id = NEW.id AND delivery_status IN ('PENDING','SENT');
    ELSE
        UPDATE review_request_recipient SET delivery_status='SKIPPED', queued_payload=NULL
        WHERE review_request_id = NEW.id AND delivery_status = 'QUEUED';
    END IF;
    FOR chat IN SELECT DISTINCT telegram_chat_id FROM review_request_recipient WHERE review_request_id = NEW.id ORDER BY 1 LOOP
        PERFORM release_review_cards(chat);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_request_release_paced_cards
    AFTER UPDATE OF status ON review_request
    FOR EACH ROW
    WHEN (OLD.status IN ('PENDING_SEND','OPEN') AND NEW.status IN ('RESOLVED','CANCELLED','EXPIRED'))
    EXECUTE FUNCTION review_request_release_paced_cards();

-- +goose Down
DROP TRIGGER IF EXISTS review_request_release_paced_cards ON review_request;
DROP FUNCTION IF EXISTS review_request_release_paced_cards();
DROP FUNCTION IF EXISTS expire_review_card_recipient(UUID);
DROP FUNCTION IF EXISTS admit_review_card(UUID, JSONB);
DROP FUNCTION IF EXISTS release_review_cards(BIGINT);
DROP FUNCTION IF EXISTS review_card_live_count(BIGINT, UUID);
DROP FUNCTION IF EXISTS lock_review_card_chat(BIGINT);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enqueue_review_card_retirement(p_review_request_id UUID, p_recipient_id UUID) RETURNS INTEGER LANGUAGE plpgsql AS $$
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
-- Cards held back by pacing would never be sent by an older worker; send them
-- now rather than lose them.
INSERT INTO job(type, payload_json, max_attempts)
SELECT 'SEND_TELEGRAM_MESSAGE', rr.queued_payload, 8
FROM review_request_recipient rr JOIN review_request r ON r.id = rr.review_request_id
WHERE rr.delivery_status = 'QUEUED' AND rr.queued_payload IS NOT NULL AND r.status IN ('PENDING_SEND','OPEN');
DROP TABLE IF EXISTS telegram_outbound_delivery;
DROP TABLE IF EXISTS review_request_message;
DROP INDEX IF EXISTS review_request_recipient_live_chat_idx;
DROP INDEX IF EXISTS review_request_recipient_queued_idx;
ALTER TABLE review_request_recipient
    DROP COLUMN IF EXISTS delivery_failed_at,
    DROP COLUMN IF EXISTS delivery_error,
    DROP COLUMN IF EXISTS card_expires_at,
    DROP COLUMN IF EXISTS queued_payload,
    DROP COLUMN IF EXISTS delivery_status;
