-- +goose Up
-- Canonical payslip forms call the date field payDate; RHICE uses the
-- transaction_at domain name used by other review forms.
-- +goose StatementBegin
CREATE FUNCTION normalize_payslip_review_telemetry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.event_type = 'REVIEW_TURN'
       AND NEW.action = 'SET_PAY_DATE'
       AND NEW.review_item_id IS NOT NULL
       AND EXISTS (
           SELECT 1 FROM review_item
           WHERE id = NEW.review_item_id
             AND review_type = 'MISSING_PAY_DATE'
             AND resolution_values ? 'payDate'
       )
       AND NOT ('transaction_at' = ANY(NEW.changed_fields)) THEN
        NEW.changed_fields := array_append(NEW.changed_fields, 'transaction_at');
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER product_telemetry_normalize_payslip_date
BEFORE INSERT ON product_telemetry_event
FOR EACH ROW EXECUTE FUNCTION normalize_payslip_review_telemetry();

-- +goose Down
DROP TRIGGER product_telemetry_normalize_payslip_date ON product_telemetry_event;
DROP FUNCTION normalize_payslip_review_telemetry();
