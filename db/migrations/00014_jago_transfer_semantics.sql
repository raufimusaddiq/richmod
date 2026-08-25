-- +goose Up
ALTER TABLE transaction DROP CONSTRAINT transaction_type_check;
ALTER TABLE transaction ADD CONSTRAINT transaction_type_check CHECK (type IN ('UNCLASSIFIED','INCOME','EXPENSE','TRANSFER','REFUND','ADJUSTMENT'));
ALTER TABLE transaction_proposal DROP CONSTRAINT transaction_proposal_proposed_type_check;
ALTER TABLE transaction_proposal ADD CONSTRAINT transaction_proposal_proposed_type_check CHECK (proposed_type IN ('UNCLASSIFIED','INCOME','EXPENSE','TRANSFER','REFUND'));

CREATE TABLE known_account (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    user_id UUID REFERENCES "user"(id),
    institution TEXT NOT NULL CHECK (length(trim(institution)) BETWEEN 1 AND 120),
    display_name TEXT NOT NULL CHECK (length(trim(display_name)) BETWEEN 1 AND 160),
    match_hint TEXT NOT NULL CHECK (length(trim(match_hint)) BETWEEN 4 AND 80),
    relationship TEXT NOT NULL CHECK (relationship IN ('OWN_ACCOUNT','HOUSEHOLD_ACCOUNT','INVESTMENT_ACCOUNT','OTHER')),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id,institution,match_hint)
);
CREATE INDEX known_account_match_idx ON known_account(household_id,active,match_hint);

-- +goose Down
DROP TABLE known_account;
ALTER TABLE transaction_proposal DROP CONSTRAINT transaction_proposal_proposed_type_check;
ALTER TABLE transaction_proposal ADD CONSTRAINT transaction_proposal_proposed_type_check CHECK (proposed_type IN ('INCOME','EXPENSE','REFUND'));
ALTER TABLE transaction DROP CONSTRAINT transaction_type_check;
ALTER TABLE transaction ADD CONSTRAINT transaction_type_check CHECK (type IN ('INCOME','EXPENSE','TRANSFER','REFUND','ADJUSTMENT'));
