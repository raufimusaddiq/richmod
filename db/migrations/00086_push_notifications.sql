-- +goose Up
-- Web Push transport: subscriptions are per device, the outbox is per user per
-- notification-worthy event. The outbox is written by AFTER INSERT triggers so
-- it shares the creating transaction automatically, including every existing
-- review/integration-action creation path in the worker and domain packages.
-- Household scope rule: a user has exactly one active household (migration
-- 00045), so a subscription row belongs to that household. Re-subscribing moves
-- household_id and retires the old household's PENDING deliveries; the trigger
-- below only enqueues for a subscription whose household matches the event.
CREATE TABLE push_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id),
    household_id UUID NOT NULL REFERENCES household(id),
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Start of the current active period. Re-subscribe resets it so events
    -- created while the device was disabled are not replayed.
    active_since TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    disabled_at TIMESTAMPTZ
);
CREATE INDEX push_subscriptions_user_live_idx
    ON push_subscriptions(user_id) WHERE disabled_at IS NULL;

CREATE TABLE push_outbox (
    id BIGSERIAL PRIMARY KEY,
    household_id UUID NOT NULL REFERENCES household(id),
    user_id UUID NOT NULL REFERENCES "user"(id),
    kind TEXT NOT NULL CHECK (kind IN ('REVIEW','INTEGRATION_ACTION')),
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    sent_at TIMESTAMPTZ,
    abandoned_at TIMESTAMPTZ
);
-- Sender claim query: only pending rows, earliest availability first.
CREATE INDEX push_outbox_pending_idx
    ON push_outbox(available_at, id)
    WHERE sent_at IS NULL AND abandoned_at IS NULL;
CREATE INDEX push_outbox_user_pending_idx
    ON push_outbox(user_id, created_at)
    WHERE sent_at IS NULL AND abandoned_at IS NULL;
-- Expiry scan: rows older than the delivery bound are abandoned at claim.
CREATE INDEX push_outbox_expiry_idx
    ON push_outbox(created_at)
    WHERE sent_at IS NULL AND abandoned_at IS NULL;

-- Per-device outcome for one outbox row. A row is finished only when every
-- delivery is terminal (SENT, DEAD, or FAILED after its own attempts), so one
-- failing device is retried instead of being hidden by another device's success.
CREATE TABLE push_outbox_delivery (
    id BIGSERIAL PRIMARY KEY,
    outbox_id BIGINT NOT NULL REFERENCES push_outbox(id),
    subscription_id UUID NOT NULL REFERENCES push_subscriptions(id),
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','SENT','DEAD','FAILED')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(outbox_id, subscription_id)
);
CREATE INDEX push_outbox_delivery_pending_idx
    ON push_outbox_delivery(outbox_id) WHERE status='PENDING';

-- One row per active household member who should be told. `available_at` is
-- deliberately delayed so a burst from one email/document batches into one
-- notification in the sender.
-- +goose StatementBegin
CREATE FUNCTION enqueue_push_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_kind TEXT;
    v_payload JSONB;
BEGIN
    IF TG_TABLE_NAME = 'review_item' THEN
        v_kind := 'REVIEW';
        v_payload := jsonb_build_object('reviewItemId', NEW.id, 'reviewType', NEW.review_type);
    ELSE
        v_kind := 'INTEGRATION_ACTION';
        v_payload := jsonb_build_object('integrationActionId', NEW.id, 'actionType', NEW.action_type, 'title', NEW.title);
    END IF;
    INSERT INTO push_outbox(household_id, user_id, kind, payload, available_at)
    SELECT NEW.household_id, hm.user_id, v_kind, v_payload, now() + interval '30 seconds'
    FROM household_member hm
    WHERE hm.household_id = NEW.household_id AND hm.active
      AND EXISTS (
          SELECT 1 FROM push_subscriptions ps
          WHERE ps.user_id = hm.user_id
            AND ps.household_id = NEW.household_id
            AND ps.disabled_at IS NULL
      );
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER review_item_push_outbox
    AFTER INSERT ON review_item
    FOR EACH ROW EXECUTE FUNCTION enqueue_push_outbox();
CREATE TRIGGER integration_action_push_outbox
    AFTER INSERT ON integration_action
    FOR EACH ROW EXECUTE FUNCTION enqueue_push_outbox();

-- +goose Down
DROP TRIGGER integration_action_push_outbox ON integration_action;
DROP TRIGGER review_item_push_outbox ON review_item;
DROP FUNCTION enqueue_push_outbox();
DROP TABLE push_outbox_delivery;
DROP TABLE push_outbox;
DROP TABLE push_subscriptions;
