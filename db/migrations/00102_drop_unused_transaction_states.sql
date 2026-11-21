-- +goose Up
-- transaction.status = 'PENDING' and transaction.type = 'ADJUSTMENT' have no producer:
-- no endpoint, worker or migration has ever written them (STATE_MACHINES.md: "never
-- inserted"). Dead states in a CHECK or in the transition trigger invite a future
-- writer to use a path nothing tests. Remove them so the schema states only what the
-- ledger can actually hold. REFUND stays (created by "Uang kembali").
--
-- Canonical financial rows are never rewritten or deleted here. If a legacy row
-- somehow exists, the migration stops instead of guessing a replacement state.

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM transaction WHERE status = 'PENDING') THEN
        RAISE EXCEPTION 'transaction rows with status PENDING exist; resolve them before dropping the state';
    END IF;
    IF EXISTS (SELECT 1 FROM transaction WHERE type = 'ADJUSTMENT') THEN
        RAISE EXCEPTION 'transaction rows with type ADJUSTMENT exist; resolve them before dropping the type';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE transaction DROP CONSTRAINT transaction_status_check;
ALTER TABLE transaction ADD CONSTRAINT transaction_status_check CHECK (status IN ('NEEDS_REVIEW', 'CONFIRMED', 'VOIDED'));

ALTER TABLE transaction DROP CONSTRAINT transaction_purpose_type_compatible;
ALTER TABLE transaction DROP CONSTRAINT transaction_type_check;
ALTER TABLE transaction ADD CONSTRAINT transaction_type_check CHECK (type IN ('UNCLASSIFIED','INCOME','EXPENSE','TRANSFER','REFUND'));
ALTER TABLE transaction ADD CONSTRAINT transaction_purpose_type_compatible CHECK (
    (type IN ('UNCLASSIFIED','INCOME','EXPENSE','REFUND') AND purpose = 'GENERAL') OR
    (type = 'TRANSFER' AND purpose IN ('INTERNAL_TRANSFER','SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE','DEBT_PRINCIPAL_PAYMENT'))
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_transaction_status_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = OLD.status THEN
        RETURN NEW;
    END IF;
    IF (OLD.status, NEW.status) IN (
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

ALTER TABLE transaction DROP CONSTRAINT transaction_purpose_type_compatible;
ALTER TABLE transaction DROP CONSTRAINT transaction_type_check;
ALTER TABLE transaction ADD CONSTRAINT transaction_type_check CHECK (type IN ('UNCLASSIFIED','INCOME','EXPENSE','TRANSFER','REFUND','ADJUSTMENT'));
ALTER TABLE transaction ADD CONSTRAINT transaction_purpose_type_compatible CHECK (
    (type IN ('UNCLASSIFIED','INCOME','EXPENSE','REFUND','ADJUSTMENT') AND purpose = 'GENERAL') OR
    (type = 'TRANSFER' AND purpose IN ('INTERNAL_TRANSFER','SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE','DEBT_PRINCIPAL_PAYMENT'))
);
ALTER TABLE transaction DROP CONSTRAINT transaction_status_check;
ALTER TABLE transaction ADD CONSTRAINT transaction_status_check CHECK (status IN ('PENDING', 'CONFIRMED', 'NEEDS_REVIEW', 'VOIDED'));
