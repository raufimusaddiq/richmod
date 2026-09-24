-- PRD §22: bounded product events. Never persist prompt, message, or financial
-- values. The triggers capture review turns and corrections atomically with the
-- corresponding canonical write.
-- +goose Up
ALTER TABLE transaction ADD COLUMN auto_confirmed_at TIMESTAMPTZ;

CREATE TABLE product_telemetry_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_event_id UUID REFERENCES source_event(id),
    transaction_id UUID REFERENCES transaction(id),
    review_item_id UUID REFERENCES review_item(id),
    event_type TEXT NOT NULL CHECK (event_type IN ('REVIEW_TURN', 'AUTO_CONFIRM_CORRECTION')),
    source_type TEXT,
    action TEXT NOT NULL,
    decision_policy_version TEXT,
    decision_source TEXT,
    changed_fields TEXT[] NOT NULL DEFAULT '{}',
    bounded_choices INTEGER NOT NULL DEFAULT 0 CHECK (bounded_choices >= 0),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_product_telemetry_household_time ON product_telemetry_event(household_id, occurred_at DESC);
CREATE INDEX idx_product_telemetry_review ON product_telemetry_event(review_item_id, occurred_at) WHERE review_item_id IS NOT NULL;
CREATE INDEX idx_product_telemetry_transaction ON product_telemetry_event(transaction_id, occurred_at) WHERE transaction_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION capture_product_review_turn() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    item review_item%ROWTYPE;
    request_id UUID;
    reply_source UUID;
    reply_field TEXT;
    source_kind TEXT;
    reply_bounded BOOLEAN := false;
BEGIN
    IF TG_TABLE_NAME = 'transaction_evidence' THEN
        IF NEW.evidence_type <> 'TELEGRAM_REVIEW_REPLY' THEN RETURN NEW; END IF;
        BEGIN
            request_id := NULLIF(NEW.metadata_json->>'review_request_id','')::uuid;
        EXCEPTION WHEN invalid_text_representation THEN
            RETURN NEW;
        END;
        IF request_id IS NULL THEN RETURN NEW; END IF;
        SELECT ri.* INTO item
        FROM review_request rr JOIN review_item ri ON ri.id=rr.review_item_id
        WHERE rr.id=request_id AND rr.household_id=(SELECT household_id FROM transaction WHERE id=NEW.transaction_id);
        IF NOT FOUND THEN RETURN NEW; END IF;
        reply_field := NEW.metadata_json->>'field';
        SELECT source_type INTO source_kind FROM source_event WHERE id=NEW.source_event_id;
        INSERT INTO product_telemetry_event(household_id,source_event_id,transaction_id,review_item_id,event_type,source_type,action,decision_policy_version,decision_source,changed_fields,bounded_choices)
        VALUES(item.household_id,NEW.source_event_id,NEW.transaction_id,item.id,'REVIEW_TURN',source_kind,
          COALESCE(NEW.metadata_json->>'detail_action',reply_field,NEW.metadata_json->>'classification','TELEGRAM_REPLY'),
          item.decision->>'decisionPolicyVersion',item.decision->>'decisionSource',
          CASE WHEN reply_field IN ('merchant','description','category','transaction_at','note','purpose','wealth_account') THEN ARRAY[reply_field] ELSE '{}'::text[] END,
          CASE WHEN NEW.metadata_json ? 'classification'
                 OR reply_field='category'
                 OR NEW.metadata_json->>'detail_action' IN ('review:ignore','review:remember','review:once')
               THEN 1 ELSE 0 END);
        RETURN NEW;
    END IF;

    IF OLD.status NOT IN ('OPEN','PENDING_SEND') OR NEW.status <> 'RESOLVED' OR NEW.resolution_action IS NULL
       OR NEW.resolution_action IN ('EMAIL_RECEIVED_AT_FALLBACK','RECONCILED_TERMINAL_TRANSACTION','LEGACY_TRANSACTION_RESOLVED') THEN
        RETURN NEW;
    END IF;
    IF EXISTS (
        SELECT 1 FROM transaction_evidence te
        JOIN review_request rr ON rr.id::text=te.metadata_json->>'review_request_id'
        WHERE rr.review_item_id=NEW.id AND te.evidence_type='TELEGRAM_REVIEW_REPLY'
    ) THEN
        RETURN NEW;
    END IF;
    -- Telegram reply evidence is the per-turn record. The resolution row is the
    -- terminal turn too; its bounded-choice marker comes only from the decision
    -- contract so machine resolutions are not credited to a human control.
    SELECT te.source_event_id,se.source_type,
      te.metadata_json ? 'classification'
        OR te.metadata_json->>'detail_action' IN ('review:ignore','review:remember','review:once')
    INTO reply_source,source_kind,reply_bounded
    FROM transaction_evidence te JOIN source_event se ON se.id=te.source_event_id
    WHERE te.transaction_id=NEW.transaction_id AND te.evidence_type='TELEGRAM_REVIEW_REPLY'
      AND te.metadata_json->>'review_request_id'=(SELECT id::text FROM review_request WHERE review_item_id=NEW.id)
    ORDER BY te.created_at DESC LIMIT 1;
    -- The resolution row counts as the terminal review interaction. Prior
    -- Telegram detail turns are counted individually from reply evidence.
    IF reply_source IS NULL THEN
        reply_source := NEW.source_event_id;
        SELECT source_type INTO source_kind FROM source_event WHERE id=reply_source;
    END IF;
    INSERT INTO product_telemetry_event(household_id,source_event_id,transaction_id,review_item_id,event_type,source_type,action,decision_policy_version,decision_source,changed_fields,bounded_choices)
    VALUES(NEW.household_id,reply_source,NEW.transaction_id,NEW.id,'REVIEW_TURN',source_kind,NEW.resolution_action,
      NEW.decision->>'decisionPolicyVersion',NEW.decision->>'decisionSource',
	  CASE WHEN NEW.resolution_action='SET_FINANCIAL_EMAIL_ENTITIES' THEN
	    ARRAY(SELECT DISTINCT field FROM jsonb_array_elements_text(COALESCE(NEW.resolution_values->'human_supplied_fields','[]'::jsonb)) AS supplied(field)
	      WHERE field IN ('account','wealth_account'))
	  ELSE ARRAY(SELECT DISTINCT normalized.field FROM jsonb_object_keys(COALESCE(NEW.resolution_values,'{}'::jsonb)) AS keys(field)
	    CROSS JOIN LATERAL (VALUES (CASE field
	      WHEN 'amount_idr' THEN 'amount' WHEN 'amountIdr' THEN 'amount'
	      WHEN 'transactionAt' THEN 'transaction_at' ELSE field END)) normalized(field)
	    WHERE normalized.field IN ('merchant','category','description','note','transaction_at','purpose','wealth_account','account','amount','type','status')) END,
	  CASE WHEN NEW.resolution_action='MERGE_REVIEW'
	         OR NEW.decision->>'interactionMode' IN ('BOUNDED_CHOICE','CONFLICT_RESOLUTION','POLICY_CHOICE') THEN 1 ELSE 0 END);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER product_telemetry_review_turn_evidence
AFTER INSERT ON transaction_evidence FOR EACH ROW EXECUTE FUNCTION capture_product_review_turn();
CREATE CONSTRAINT TRIGGER product_telemetry_review_turn_resolution
AFTER UPDATE ON review_item DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION capture_product_review_turn();

-- +goose StatementBegin
CREATE FUNCTION capture_auto_confirm_correction() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    changed TEXT[] := '{}'::text[];
    source_id UUID;
    source_kind TEXT;
    policy TEXT;
    decision TEXT;
BEGIN
    IF OLD.auto_confirmed_at IS NULL THEN RETURN NEW; END IF;
    IF OLD.amount IS DISTINCT FROM NEW.amount THEN changed := array_append(changed,'amount'); END IF;
    IF OLD.type IS DISTINCT FROM NEW.type THEN changed := array_append(changed,'type'); END IF;
    IF OLD.transaction_at IS DISTINCT FROM NEW.transaction_at THEN changed := array_append(changed,'transaction_at'); END IF;
    IF OLD.category_id IS DISTINCT FROM NEW.category_id THEN changed := array_append(changed,'category'); END IF;
    IF OLD.merchant_id IS DISTINCT FROM NEW.merchant_id THEN changed := array_append(changed,'merchant'); END IF;
    IF OLD.description IS DISTINCT FROM NEW.description THEN changed := array_append(changed,'description'); END IF;
    IF OLD.note IS DISTINCT FROM NEW.note THEN changed := array_append(changed,'note'); END IF;
    IF OLD.purpose IS DISTINCT FROM NEW.purpose THEN changed := array_append(changed,'purpose'); END IF;
    IF OLD.related_wealth_account_id IS DISTINCT FROM NEW.related_wealth_account_id THEN changed := array_append(changed,'wealth_account'); END IF;
    IF OLD.account_id IS DISTINCT FROM NEW.account_id THEN changed := array_append(changed,'account'); END IF;
    IF OLD.status IS DISTINCT FROM NEW.status THEN changed := array_append(changed,'status'); END IF;
    IF cardinality(changed)=0 THEN RETURN NEW; END IF;

    SELECT te.source_event_id,se.source_type INTO source_id,source_kind
    FROM transaction_evidence te JOIN source_event se ON se.id=te.source_event_id
    WHERE te.transaction_id=OLD.id ORDER BY te.created_at LIMIT 1;
    SELECT COALESCE(tp.metadata_json->>'decision_policy_version',tp.metadata_json->>'policy_version',jd.policy_version,''),
           COALESCE(tp.metadata_json->>'decision_source','')
    INTO policy,decision
    FROM transaction_evidence te
    LEFT JOIN transaction_proposal tp ON tp.id::text=te.metadata_json->>'proposal_id'
    LEFT JOIN LATERAL (SELECT policy_version FROM judgment_decision WHERE source_event_id=te.source_event_id ORDER BY created_at DESC LIMIT 1) jd ON true
    WHERE te.transaction_id=OLD.id ORDER BY te.created_at LIMIT 1;
    INSERT INTO product_telemetry_event(household_id,source_event_id,transaction_id,event_type,source_type,action,decision_policy_version,decision_source,changed_fields)
    VALUES(NEW.household_id,source_id,NEW.id,'AUTO_CONFIRM_CORRECTION',source_kind,'TRANSACTION_CORRECTED',policy,decision,changed);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER product_telemetry_auto_confirm_correction
AFTER UPDATE OF amount,type,transaction_at,category_id,merchant_id,description,note,purpose,related_wealth_account_id,account_id,status
ON transaction FOR EACH ROW EXECUTE FUNCTION capture_auto_confirm_correction();

-- +goose Down
DROP TRIGGER product_telemetry_auto_confirm_correction ON transaction;
DROP FUNCTION capture_auto_confirm_correction();
DROP TRIGGER product_telemetry_review_turn_resolution ON review_item;
DROP TRIGGER product_telemetry_review_turn_evidence ON transaction_evidence;
DROP FUNCTION capture_product_review_turn();
DROP TABLE product_telemetry_event;
ALTER TABLE transaction DROP COLUMN auto_confirmed_at;
