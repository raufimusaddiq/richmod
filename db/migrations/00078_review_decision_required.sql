-- +goose Up
-- Every new review_item carries a complete ReviewDecision contract: a non-empty
-- reasonCode and a non-empty allowedActions array (the same rule as Go's
-- reviewdec.Decision.Validate). Rows created before the contract existed have
-- decision IS NULL and are historical records: they stay readable and are never
-- rewritten, so this is a trigger rather than NOT NULL/CHECK. On UPDATE a
-- legacy NULL row stays editable (status, resolution) but can never become
-- active (OPEN/PENDING_SEND) again, so every active item carries a contract;
-- a row that already had a decision cannot have it erased or degraded.
--
-- Precondition: no active item may violate the contract when this runs (the
-- Inbox and Telegram render only stored contracts). Fail loudly instead of
-- guessing a decision for such a row.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM review_item
        WHERE status IN ('OPEN','PENDING_SEND')
          AND (decision IS NULL
               OR COALESCE(btrim(decision->>'reasonCode'),'') = ''
               OR jsonb_typeof(decision->'allowedActions') IS DISTINCT FROM 'array'
               OR decision->'allowedActions' = '[]'::jsonb)
    ) THEN
        RAISE EXCEPTION 'active review_item rows without a complete decision exist; resolve them before applying 00078';
    END IF;
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION review_item_require_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    actions_present boolean;
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.decision IS NULL THEN
        IF NEW.status IN ('OPEN','PENDING_SEND') THEN
            RAISE EXCEPTION 'a review_item without a decision contract cannot be active'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.decision IS NULL THEN
        RAISE EXCEPTION 'review_item.decision is required'
            USING ERRCODE = 'check_violation';
    END IF;
    IF jsonb_typeof(NEW.decision->'reasonCode') IS DISTINCT FROM 'string'
       OR btrim(NEW.decision->>'reasonCode') = '' THEN
        RAISE EXCEPTION 'review_item.decision.reasonCode must be a non-empty string'
            USING ERRCODE = 'check_violation';
    END IF;
    -- PL/pgSQL ends an IF condition at the first THEN, so the CASE guard (which
    -- keeps jsonb_array_length off non-arrays) is evaluated in an assignment.
    actions_present := CASE WHEN jsonb_typeof(NEW.decision->'allowedActions') = 'array'
                            THEN jsonb_array_length(NEW.decision->'allowedActions') > 0
                            ELSE false END;
    IF NOT actions_present THEN
        RAISE EXCEPTION 'review_item.decision.allowedActions must be a non-empty array'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER review_item_require_decision
    BEFORE INSERT OR UPDATE OF decision, status ON review_item
    FOR EACH ROW EXECUTE FUNCTION review_item_require_decision();

-- +goose Down
DROP TRIGGER IF EXISTS review_item_require_decision ON review_item;
DROP FUNCTION IF EXISTS review_item_require_decision();
