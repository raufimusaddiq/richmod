-- +goose Up
-- Transaction status is a state machine whose legal moves lived only in scattered
-- guarded UPDATEs. Encode it once, in the database, so no writer (web, Telegram,
-- a future script) can resurrect a voided transaction or move one back to PENDING.
--
--   PENDING      -> CONFIRMED | NEEDS_REVIEW | VOIDED
--   NEEDS_REVIEW -> CONFIRMED | VOIDED
--   CONFIRMED    -> VOIDED | NEEDS_REVIEW   (a disputed confirmation is reopened)
--   VOIDED       -> NEEDS_REVIEW   (only a reversed merge reopens a voided source)
--
-- salary_event.status is a denormalised copy of its transaction's status; it
-- stayed CONFIRMED after the payslip transaction was voided, so a voided salary
-- still anchored salary cycles and swallowed a re-sent payslip as a duplicate.
-- A trigger now keeps it in step.

-- +goose StatementBegin
CREATE FUNCTION enforce_transaction_status_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = OLD.status THEN
        RETURN NEW;
    END IF;
    IF (OLD.status, NEW.status) IN (
        ('PENDING','CONFIRMED'), ('PENDING','NEEDS_REVIEW'), ('PENDING','VOIDED'),
        ('NEEDS_REVIEW','CONFIRMED'), ('NEEDS_REVIEW','VOIDED'),
        ('CONFIRMED','VOIDED'), ('CONFIRMED','NEEDS_REVIEW'),
        ('VOIDED','NEEDS_REVIEW')
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'illegal transaction status transition % -> %', OLD.status, NEW.status
        USING ERRCODE = 'check_violation', TABLE = 'transaction', COLUMN = 'status';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER transaction_status_transition
    BEFORE UPDATE OF status ON transaction
    FOR EACH ROW EXECUTE FUNCTION enforce_transaction_status_transition();

-- +goose StatementBegin
CREATE FUNCTION sync_salary_event_status() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'VOIDED' THEN
        UPDATE salary_event SET status = 'VOIDED' WHERE transaction_id = NEW.id AND status <> 'VOIDED';
    ELSIF NEW.status = 'CONFIRMED' THEN
        UPDATE salary_event SET status = 'CONFIRMED' WHERE transaction_id = NEW.id AND status <> 'CONFIRMED';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER transaction_salary_event_status
    AFTER UPDATE OF status ON transaction
    FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION sync_salary_event_status();

UPDATE salary_event se SET status = 'VOIDED'
FROM transaction t WHERE t.id = se.transaction_id AND t.status = 'VOIDED' AND se.status <> 'VOIDED';

-- +goose Down
DROP TRIGGER transaction_salary_event_status ON transaction;
DROP FUNCTION sync_salary_event_status();
DROP TRIGGER transaction_status_transition ON transaction;
DROP FUNCTION enforce_transaction_status_transition();
