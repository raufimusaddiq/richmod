-- +goose Up
CREATE TABLE salary_source (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    user_id UUID REFERENCES "user"(id),
    employer TEXT NOT NULL,
    normalized_employer TEXT NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX salary_source_employer_unique ON salary_source(household_id, normalized_employer) WHERE active;
CREATE UNIQUE INDEX salary_source_primary_unique ON salary_source(household_id) WHERE active AND is_primary;

CREATE TABLE salary_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    salary_source_id UUID NOT NULL REFERENCES salary_source(id),
    household_id UUID NOT NULL REFERENCES household(id),
    payroll_period DATE NOT NULL,
    pay_date DATE NOT NULL,
    net_pay NUMERIC(19,2) NOT NULL CHECK (net_pay > 0),
    currency TEXT NOT NULL DEFAULT 'IDR' CHECK (currency = 'IDR'),
    transaction_id UUID NOT NULL UNIQUE REFERENCES transaction(id),
    status TEXT NOT NULL CHECK (status IN ('CONFIRMED','VOIDED')),
    source_event_id UUID NOT NULL REFERENCES source_event(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (salary_source_id, payroll_period)
);
CREATE INDEX salary_event_cycle_order ON salary_event(salary_source_id, pay_date);

-- +goose Down
DROP TABLE salary_event;
DROP TABLE salary_source;
