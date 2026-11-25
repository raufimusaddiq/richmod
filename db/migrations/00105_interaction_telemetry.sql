-- +goose Up
ALTER TABLE product_telemetry_event DROP CONSTRAINT IF EXISTS product_telemetry_event_event_type_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_event_type_check
  CHECK (event_type IN ('REVIEW_TURN', 'AUTO_CONFIRM_CORRECTION', 'CEU_BINDING', 'UI_EVENT', 'INTERACTION'));

ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_interaction_check
  CHECK (
    event_type <> 'INTERACTION'
    OR (
      action IN ('ANSWERED', 'UNMATCHED', 'AMBIGUOUS', 'CLOSED_CARD_REPLY', 'REPEATED_INPUT', 'STEP_SHOWN', 'FINISHED_ON_WEB')
      AND (source_type IS NULL OR source_type IN ('TAP', 'REPLY', 'THREAD_REPLY', 'PLAIN', 'LABEL_TEXT'))
      AND (source_type IS NULL OR action = 'ANSWERED')
      AND screen IS NULL
      AND changed_fields = '{}' AND bounded_choices = 0
    )
  );

CREATE UNIQUE INDEX product_telemetry_interaction_once ON product_telemetry_event(source_event_id, action)
  WHERE event_type = 'INTERACTION' AND source_event_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION capture_review_finished_on_web() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF current_setting('application_name', true) <> 'richmod-api' THEN
        RETURN NULL;
    END IF;
    IF NEW.resolved_by_user_id IS NULL THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM review_request_message m JOIN review_request r ON r.id=m.review_request_id
        WHERE r.review_item_id=NEW.id
    ) THEN
        RETURN NULL;
    END IF;
    BEGIN
        INSERT INTO product_telemetry_event(household_id,transaction_id,review_item_id,event_type,action)
        VALUES(NEW.household_id,NEW.transaction_id,NEW.id,'INTERACTION','FINISHED_ON_WEB');
    EXCEPTION WHEN OTHERS THEN
        RETURN NULL;
    END;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER product_telemetry_finished_on_web
AFTER UPDATE OF status ON review_item DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
WHEN (OLD.status IN ('OPEN','PENDING_SEND') AND NEW.status = 'RESOLVED')
EXECUTE FUNCTION capture_review_finished_on_web();

-- +goose Down
DROP TRIGGER product_telemetry_finished_on_web ON review_item;
DROP FUNCTION capture_review_finished_on_web();
DROP INDEX product_telemetry_interaction_once;
DELETE FROM product_telemetry_event WHERE event_type='INTERACTION';
ALTER TABLE product_telemetry_event DROP CONSTRAINT product_telemetry_event_interaction_check;
ALTER TABLE product_telemetry_event DROP CONSTRAINT product_telemetry_event_event_type_check;
ALTER TABLE product_telemetry_event ADD CONSTRAINT product_telemetry_event_event_type_check
  CHECK (event_type IN ('REVIEW_TURN', 'AUTO_CONFIRM_CORRECTION', 'CEU_BINDING', 'UI_EVENT'));
