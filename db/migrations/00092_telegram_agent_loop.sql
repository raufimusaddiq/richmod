-- +goose Up
-- Telegram agent loop hardening (ADR-033 / ADR-052 amendments, 2026-11).

-- Corrections may also change the amount and merchant of a transaction the
-- household entered itself; Go checks the evidence before staging and again
-- under lock before applying. NULL keeps the current value.
ALTER TABLE telegram_pending_action ADD COLUMN proposed_amount NUMERIC(20,0) CHECK (proposed_amount > 0);
ALTER TABLE telegram_pending_action ADD COLUMN proposed_merchant TEXT CHECK (proposed_merchant IS NULL OR length(proposed_merchant) BETWEEN 1 AND 160);

-- A staged correction or batch waits 30 minutes for its confirmation instead
-- of 5: people answer later than that. Go sets the same interval explicitly.
ALTER TABLE telegram_pending_action ALTER COLUMN expires_at SET DEFAULT now()+interval '30 minutes';
ALTER TABLE telegram_pending_batch ALTER COLUMN expires_at SET DEFAULT now()+interval '30 minutes';

-- A typed answer that deterministically fits more than one open review card
-- (for example a category name while two category cards are open) is not
-- guessed: Richmod asks which card with one button per card. The choice row
-- holds the answer Go parsed; each option is an opaque button token bound to
-- one review request. Household-, Telegram user- and chat-scoped; expires.
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

-- +goose Down
DROP TABLE telegram_pending_card_choice_option;
DROP TABLE telegram_pending_card_choice;
ALTER TABLE telegram_pending_batch ALTER COLUMN expires_at SET DEFAULT now()+interval '5 minutes';
ALTER TABLE telegram_pending_action ALTER COLUMN expires_at SET DEFAULT now()+interval '5 minutes';
ALTER TABLE telegram_pending_action DROP COLUMN proposed_merchant;
ALTER TABLE telegram_pending_action DROP COLUMN proposed_amount;
