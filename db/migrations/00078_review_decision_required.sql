-- +goose Up
-- Every new review_item carries a complete ReviewDecision contract: a non-empty
-- reasonCode and a non-empty allowedActions array (the same rule as Go's
-- reviewdec.Decision.Validate). Rows created before the contract existed have
-- decision IS NULL and are historical records: they stay readable and are never
-- rewritten, so this is a trigger rather than NOT NULL/CHECK. An UPDATE is
-- checked only when the row already had a decision, which keeps the legacy NULL
-- rows editable (status, resolution) while forbidding a valid contract from being
-- erased or degraded.
-- +goose StatementBegin
CREATE FUNCTION review_item_require_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    actions_present boolean;
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.decision IS NULL THEN
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
    BEFORE INSERT OR UPDATE OF decision ON review_item
    FOR EACH ROW EXECUTE FUNCTION review_item_require_decision();

-- +goose Down
DROP TRIGGER IF EXISTS review_item_require_decision ON review_item;
DROP FUNCTION IF EXISTS review_item_require_decision();
