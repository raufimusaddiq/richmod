-- +goose Up
CREATE TABLE salary_pending_choice (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), household_id UUID NOT NULL REFERENCES household(id),
 telegram_user_id BIGINT NOT NULL, telegram_chat_id BIGINT NOT NULL, transaction_id UUID NOT NULL REFERENCES transaction(id),
 employer TEXT NOT NULL, payroll_period DATE NOT NULL, pay_date DATE NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('PENDING','PRIMARY','ORDINARY','IGNORED')), expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '7 days', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), resolved_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX salary_pending_choice_chat ON salary_pending_choice(telegram_user_id,telegram_chat_id) WHERE status='PENDING';
-- +goose Down
DROP TABLE salary_pending_choice;
