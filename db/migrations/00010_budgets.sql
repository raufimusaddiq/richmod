-- +goose Up
CREATE TABLE budget (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    category_id UUID NOT NULL REFERENCES category(id),
    monthly_amount NUMERIC(20,0) NOT NULL CHECK (monthly_amount > 0),
    currency CHAR(3) NOT NULL DEFAULT 'IDR' CHECK (currency = 'IDR'),
    start_month DATE NOT NULL DEFAULT (date_trunc('month', CURRENT_DATE)::date),
    end_month DATE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (extract(day FROM start_month) = 1),
    CHECK (end_month IS NULL OR (extract(day FROM end_month) = 1 AND end_month >= start_month))
);

CREATE UNIQUE INDEX budget_active_household_category_unique
    ON budget(household_id,category_id) WHERE active;
CREATE INDEX budget_household_period_idx
    ON budget(household_id,start_month,end_month);

-- +goose Down
DROP TABLE budget;
