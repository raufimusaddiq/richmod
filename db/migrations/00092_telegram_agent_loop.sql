-- +goose Up

ALTER TABLE telegram_pending_action ADD COLUMN proposed_amount NUMERIC(20,0) CHECK (proposed_amount > 0);
ALTER TABLE telegram_pending_action ADD COLUMN proposed_merchant TEXT CHECK (proposed_merchant IS NULL OR length(proposed_merchant) BETWEEN 1 AND 160);

ALTER TABLE telegram_pending_action ALTER COLUMN expires_at SET DEFAULT now()+interval '30 minutes';
ALTER TABLE telegram_pending_batch ALTER COLUMN expires_at SET DEFAULT now()+interval '30 minutes';

CREATE TABLE telegram_pending_card_choice (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    telegram_user_id BIGINT NOT NULL,
    telegram_chat_id BIGINT NOT NULL,
    source_event_id UUID NOT NULL,
    answer_field TEXT NOT NULL CHECK (answer_field IN ('category_slug','transaction_at')),
    answer_value TEXT NOT NULL CHECK (length(answer_value) BETWEEN 1 AND 160),
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RESOLVED','CANCELLED','EXPIRED')),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '30 minutes',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    FOREIGN KEY (household_id, source_event_id) REFERENCES source_event(household_id, id)
);
CREATE UNIQUE INDEX telegram_pending_card_choice_source_key ON telegram_pending_card_choice (source_event_id);

CREATE TABLE telegram_pending_card_choice_option (
    choice_id UUID NOT NULL REFERENCES telegram_pending_card_choice(id),
    position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 10),
    token UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    review_request_id UUID NOT NULL REFERENCES review_request(id),
    PRIMARY KEY (choice_id, position)
);

ALTER TABLE job ADD COLUMN deferrals INTEGER NOT NULL DEFAULT 0 CHECK (deferrals >= 0);

-- +goose Down
ALTER TABLE job DROP COLUMN deferrals;
DROP TABLE telegram_pending_card_choice_option;
DROP TABLE telegram_pending_card_choice;
ALTER TABLE telegram_pending_batch ALTER COLUMN expires_at SET DEFAULT now()+interval '5 minutes';
ALTER TABLE telegram_pending_action ALTER COLUMN expires_at SET DEFAULT now()+interval '5 minutes';
ALTER TABLE telegram_pending_action DROP COLUMN proposed_merchant;
ALTER TABLE telegram_pending_action DROP COLUMN proposed_amount;
