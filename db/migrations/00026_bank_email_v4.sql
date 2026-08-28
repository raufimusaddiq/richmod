-- +goose Up
ALTER TABLE account ADD COLUMN system_managed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE account ADD COLUMN system_key TEXT;
CREATE UNIQUE INDEX account_system_key_unique ON account(household_id,system_key) WHERE system_key IS NOT NULL;

CREATE TABLE bank_email_listener (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    bank_name TEXT NOT NULL CHECK (length(trim(bank_name)) BETWEEN 1 AND 120),
    sender_address TEXT NOT NULL CHECK (sender_address = lower(trim(sender_address))),
    tracking_policy TEXT NOT NULL DEFAULT 'SPENDING_ONLY' CHECK (tracking_policy = 'SPENDING_ONLY'),
    account_id UUID REFERENCES account(id),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX bank_email_listener_sender_unique ON bank_email_listener(household_id,sender_address) WHERE active;

CREATE TABLE bank_email_event (
    source_event_id UUID PRIMARY KEY REFERENCES source_event(id),
    listener_id UUID NOT NULL REFERENCES bank_email_listener(id),
    observed_sender TEXT NOT NULL,
    gmail_message_id TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    email_date TEXT NOT NULL DEFAULT '',
    authentication_results TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE bank_email_extraction (
    source_event_id UUID PRIMARY KEY REFERENCES source_event(id),
    listener_id UUID NOT NULL REFERENCES bank_email_listener(id),
    protocol TEXT NOT NULL,
    gateway_model TEXT,
    tool_schema_version TEXT NOT NULL,
    output_json JSONB NOT NULL,
    validation_status TEXT NOT NULL,
    policy_result TEXT,
    shadow_output_json JSONB,
    shadow_agreement TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE review_conversation DROP CONSTRAINT review_conversation_state_check;
ALTER TABLE review_conversation ADD CONSTRAINT review_conversation_state_check CHECK (state IN ('AWAITING_MERCHANT','AWAITING_CATEGORY','AWAITING_DETAIL','AWAITING_PURPOSE','AWAITING_CONFIRMATION','RESOLVED'));

-- +goose Down
ALTER TABLE review_conversation DROP CONSTRAINT review_conversation_state_check;
ALTER TABLE review_conversation ADD CONSTRAINT review_conversation_state_check CHECK (state IN ('AWAITING_PURPOSE','AWAITING_CATEGORY','AWAITING_CONFIRMATION','RESOLVED'));
DROP TABLE bank_email_extraction;
DROP TABLE bank_email_event;
DROP INDEX account_system_key_unique;
ALTER TABLE account DROP COLUMN system_key;
ALTER TABLE account DROP COLUMN system_managed;
DROP TABLE bank_email_listener;
