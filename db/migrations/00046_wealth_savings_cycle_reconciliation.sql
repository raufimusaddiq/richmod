-- +goose Up

ALTER TABLE transaction ADD COLUMN purpose TEXT;
UPDATE transaction SET purpose = CASE WHEN type = 'TRANSFER' THEN 'INTERNAL_TRANSFER' ELSE 'GENERAL' END;
-- +goose StatementBegin
CREATE FUNCTION transaction_default_purpose() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.purpose IS NULL THEN
        NEW.purpose := CASE WHEN NEW.type = 'TRANSFER' THEN 'INTERNAL_TRANSFER' ELSE 'GENERAL' END;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER transaction_default_purpose_before_write
    BEFORE INSERT OR UPDATE OF type, purpose ON transaction
    FOR EACH ROW EXECUTE FUNCTION transaction_default_purpose();
-- +goose StatementEnd
ALTER TABLE transaction ALTER COLUMN purpose SET NOT NULL;
ALTER TABLE transaction ADD CONSTRAINT transaction_purpose_allowed CHECK (purpose IN ('GENERAL','INTERNAL_TRANSFER','SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE','DEBT_PRINCIPAL_PAYMENT'));
ALTER TABLE transaction ADD CONSTRAINT transaction_purpose_type_compatible CHECK (
    (type IN ('UNCLASSIFIED','INCOME','EXPENSE','REFUND','ADJUSTMENT') AND purpose = 'GENERAL') OR
    (type = 'TRANSFER' AND purpose IN ('INTERNAL_TRANSFER','SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE','DEBT_PRINCIPAL_PAYMENT'))
);

CREATE TABLE wealth_account (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    name TEXT NOT NULL,
    institution TEXT,
    side TEXT NOT NULL CHECK (side IN ('ASSET','LIABILITY')),
    wealth_type TEXT NOT NULL CHECK (wealth_type IN ('BANK','CASH','EWALLET','MUTUAL_FUND','GOLD','BROKERAGE','DEPOSIT','CRYPTO','LOAN','OTHER')),
    usage_role TEXT NOT NULL CHECK (usage_role IN ('TRANSACTIONAL','SAVINGS','INVESTMENT','OTHER')),
    owner_user_id UUID REFERENCES "user"(id),
    linked_account_id UUID REFERENCES account(id),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id, name),
    CONSTRAINT wealth_account_side_type_check CHECK ((side = 'ASSET' AND wealth_type IN ('BANK','CASH','EWALLET','MUTUAL_FUND','GOLD','BROKERAGE','DEPOSIT','CRYPTO','OTHER')) OR (side = 'LIABILITY' AND wealth_type IN ('LOAN','OTHER'))),
    CONSTRAINT wealth_account_liability_role_check CHECK (side = 'ASSET' OR usage_role = 'OTHER')
);
CREATE UNIQUE INDEX wealth_account_active_link_unique ON wealth_account(linked_account_id) WHERE active AND linked_account_id IS NOT NULL;
CREATE INDEX wealth_account_household_active_role_name_idx ON wealth_account(household_id, active, usage_role, name);

ALTER TABLE transaction ADD COLUMN related_wealth_account_id UUID REFERENCES wealth_account(id);
ALTER TABLE known_account ADD COLUMN wealth_account_id UUID REFERENCES wealth_account(id);

CREATE TABLE wealth_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    observed_at TIMESTAMPTZ NOT NULL,
    created_by_user_id UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id, observed_at)
);
CREATE INDEX wealth_snapshot_household_observed_idx ON wealth_snapshot(household_id, observed_at DESC);

CREATE TABLE wealth_snapshot_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id UUID NOT NULL REFERENCES wealth_snapshot(id),
    wealth_account_id UUID NOT NULL REFERENCES wealth_account(id),
    value_idr NUMERIC(20,0) NOT NULL CHECK (value_idr >= 0),
    quantity NUMERIC(30,10) CHECK (quantity >= 0),
    unit TEXT,
    unit_price_idr NUMERIC(20,0) CHECK (unit_price_idr >= 0),
    source TEXT NOT NULL CHECK (source IN ('MANUAL','DOCUMENT','SYSTEM')),
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (snapshot_id, wealth_account_id)
);
CREATE INDEX wealth_snapshot_item_account_snapshot_idx ON wealth_snapshot_item(wealth_account_id, snapshot_id);

CREATE TABLE cycle_residual_case (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    start_salary_event_id UUID NOT NULL REFERENCES salary_event(id),
    end_salary_event_id UUID NOT NULL REFERENCES salary_event(id),
    cycle_start DATE NOT NULL,
    cycle_end DATE NOT NULL,
    basis_income_idr NUMERIC(20,0) NOT NULL,
    basis_expense_idr NUMERIC(20,0) NOT NULL,
    basis_savings_idr NUMERIC(20,0) NOT NULL,
    basis_residual_idr NUMERIC(20,0) NOT NULL CHECK (basis_residual_idr > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id, start_salary_event_id, end_salary_event_id),
    CHECK (cycle_end > cycle_start)
);
CREATE INDEX cycle_residual_case_household_end_idx ON cycle_residual_case(household_id, cycle_end DESC);

CREATE TABLE cycle_residual_allocation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_residual_case_id UUID NOT NULL REFERENCES cycle_residual_case(id),
    wealth_account_id UUID NOT NULL REFERENCES wealth_account(id),
    amount_idr NUMERIC(20,0) NOT NULL CHECK (amount_idr > 0),
    note TEXT,
    created_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cycle_residual_allocation_case_idx ON cycle_residual_allocation(cycle_residual_case_id);
CREATE UNIQUE INDEX cycle_residual_allocation_case_account_unique ON cycle_residual_allocation(cycle_residual_case_id,wealth_account_id);

-- Idempotency key for salary-triggered residual generation jobs.
CREATE UNIQUE INDEX job_cycle_residual_event_unique ON job ((payload_json->>'end_salary_event_id')) WHERE type='GENERATE_CYCLE_RESIDUAL_REVIEW' AND status IN ('PENDING','RUNNING','SUCCEEDED');

ALTER TABLE review_item ADD COLUMN cycle_residual_case_id UUID REFERENCES cycle_residual_case(id);
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_review_type_check;
ALTER TABLE review_item ADD CONSTRAINT review_item_review_type_check CHECK (review_type IN ('UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION','DOCUMENT_CLASSIFICATION','PAYSLIP_CONFIRMATION','MISSING_PAY_DATE','SALARY_SOURCE_CONFIRMATION','UNKNOWN_BANK_TEMPLATE','INVOICE_PAYMENT_STATUS','CYCLE_RESIDUAL_ALLOCATION'));
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_check;
-- Preserve historical subject provenance. Resolve ambiguous legacy rows before
-- enforcing the one-canonical-subject invariant instead of erasing their links.
UPDATE review_item
SET status = 'RESOLVED',
    resolved_at = COALESCE(resolved_at, now()),
    resolution_action = COALESCE(resolution_action, 'LEGACY_MULTIPLE_SUBJECTS'),
    updated_at = now()
WHERE ((transaction_id IS NOT NULL)::int + (proposal_id IS NOT NULL)::int + (source_event_id IS NOT NULL)::int + (document_id IS NOT NULL)::int) > 1;
ALTER TABLE review_item ADD CONSTRAINT review_item_exactly_one_subject_check CHECK (
    status = 'RESOLVED' OR
    ((transaction_id IS NOT NULL)::int + (proposal_id IS NOT NULL)::int + (source_event_id IS NOT NULL)::int + (document_id IS NOT NULL)::int + (cycle_residual_case_id IS NOT NULL)::int) = 1
);
CREATE UNIQUE INDEX review_item_open_cycle_residual_unique ON review_item(cycle_residual_case_id) WHERE cycle_residual_case_id IS NOT NULL AND status IN ('PENDING_SEND','OPEN');
CREATE INDEX review_item_cycle_residual_idx ON review_item(cycle_residual_case_id) WHERE cycle_residual_case_id IS NOT NULL;

ALTER TABLE review_request ALTER COLUMN transaction_id DROP NOT NULL;
ALTER TABLE review_request ALTER COLUMN telegram_chat_id DROP NOT NULL;
ALTER TABLE review_request DROP CONSTRAINT IF EXISTS review_request_review_type_check;
ALTER TABLE review_request ADD CONSTRAINT review_request_review_type_check CHECK (review_type IN ('UNKNOWN_MERCHANT','UNKNOWN_PURPOSE','AMBIGUOUS_CATEGORY','POSSIBLE_DUPLICATE','CONFLICTING_EVIDENCE','UNKNOWN_EMAIL_TEMPLATE','RECEIPT_MISMATCH','DOCUMENT_EXTRACTION_LOW_CONFIDENCE','TRANSFER_CLASSIFICATION','MANUAL_CORRECTION','CYCLE_RESIDUAL_ALLOCATION'));
ALTER TABLE review_request ADD CONSTRAINT review_request_subject_check CHECK (transaction_id IS NOT NULL OR review_item_id IS NOT NULL);

-- +goose Down
DROP INDEX review_item_cycle_residual_idx;
DROP INDEX review_item_open_cycle_residual_unique;
ALTER TABLE review_request DROP CONSTRAINT review_request_subject_check;
ALTER TABLE review_request ALTER COLUMN telegram_chat_id SET NOT NULL;
ALTER TABLE review_request ALTER COLUMN transaction_id SET NOT NULL;
ALTER TABLE review_item DROP CONSTRAINT review_item_exactly_one_subject_check;
ALTER TABLE review_item DROP CONSTRAINT review_item_review_type_check;
ALTER TABLE review_item DROP COLUMN cycle_residual_case_id;
DROP INDEX job_cycle_residual_event_unique;
DROP INDEX cycle_residual_allocation_case_account_unique;
DROP TABLE cycle_residual_allocation;
DROP TABLE cycle_residual_case;
DROP INDEX wealth_snapshot_item_account_snapshot_idx;
DROP TABLE wealth_snapshot_item;
DROP INDEX wealth_snapshot_household_observed_idx;
DROP TABLE wealth_snapshot;
ALTER TABLE known_account DROP COLUMN wealth_account_id;
ALTER TABLE transaction DROP COLUMN related_wealth_account_id;
DROP INDEX wealth_account_active_link_unique;
DROP INDEX wealth_account_household_active_role_name_idx;
DROP TABLE wealth_account;
ALTER TABLE transaction DROP CONSTRAINT transaction_purpose_type_compatible;
ALTER TABLE transaction DROP CONSTRAINT transaction_purpose_allowed;
ALTER TABLE transaction DROP COLUMN purpose;
