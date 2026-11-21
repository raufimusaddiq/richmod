-- +goose Up

CREATE INDEX reconciliation_merge_active_target_idx
    ON reconciliation_merge (target_transaction_id) WHERE status = 'ACTIVE';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_transaction_status_transition() RETURNS trigger
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
        IF NEW.status = 'VOIDED'
           AND EXISTS (SELECT 1 FROM reconciliation_merge m
                       WHERE m.target_transaction_id = NEW.id AND m.status = 'ACTIVE')
           AND NOT EXISTS (SELECT 1 FROM reconciliation_merge s
                           WHERE s.source_transaction_id = NEW.id AND s.status = 'ACTIVE') THEN
            RAISE EXCEPTION 'transaction is the target of an active reconciliation merge'
                USING ERRCODE = 'check_violation', TABLE = 'transaction', CONSTRAINT = 'transaction_active_merge_target';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'illegal transaction status transition % -> %', OLD.status, NEW.status
        USING ERRCODE = 'check_violation', TABLE = 'transaction', COLUMN = 'status';
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP INDEX reconciliation_merge_active_target_idx;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_transaction_status_transition() RETURNS trigger
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
