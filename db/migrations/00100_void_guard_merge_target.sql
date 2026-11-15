-- +goose Up
-- A reconciliation merge folds a duplicate (the SOURCE, voided by the merge) into a
-- live TARGET. Voiding the target while the merge is ACTIVE would leave the real event
-- with no live transaction: the source is voided, the target is voided, and the copied
-- evidence sits on a voided row. Refuse it in the database so no writer (web void,
-- review reject, evidence dispute, Telegram) can do it; callers map the error to a
-- clean refusal (reviewdomain.ErrMergeTargetActive).
--
-- Exception: a transaction that is itself the SOURCE of an ACTIVE merge (X -> A, then
-- A -> B) is being voided by that second merge, and its event lives on in B. B is then
-- the protected target.

-- The guard looks up ACTIVE merges by target on every move to VOIDED (the source side already has
-- reconciliation_merge_active_source_unique).
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
