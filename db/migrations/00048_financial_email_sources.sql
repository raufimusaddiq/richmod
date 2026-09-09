-- +goose Up
CREATE TABLE financial_email_source (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    provider_name TEXT NOT NULL CHECK (length(trim(provider_name)) BETWEEN 1 AND 120),
    sender_address TEXT NOT NULL CHECK (sender_address = lower(trim(sender_address))),
    processing_mode TEXT NOT NULL DEFAULT 'FINANCIAL_LLM' CHECK (processing_mode='FINANCIAL_LLM'),
    capabilities TEXT[] NOT NULL DEFAULT ARRAY['CASH_MOVEMENT','WEALTH_VALUE']::text[],
    default_wealth_account_id UUID REFERENCES wealth_account(id),
    status TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','ACTIVE','DISABLED')),
    last_received_at TIMESTAMPTZ,
    created_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (capabilities <@ ARRAY['CASH_MOVEMENT','WEALTH_VALUE']::text[] AND cardinality(capabilities)>0)
);
CREATE INDEX financial_email_source_household_idx ON financial_email_source(household_id,status,created_at DESC);
CREATE TABLE email_sender_route (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    sender_address TEXT NOT NULL CHECK (sender_address = lower(trim(sender_address))),
    route_kind TEXT NOT NULL CHECK (route_kind IN ('BANK','FINANCIAL')),
    bank_listener_id UUID REFERENCES bank_email_listener(id),
    financial_source_id UUID REFERENCES financial_email_source(id),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((route_kind='BANK' AND bank_listener_id IS NOT NULL AND financial_source_id IS NULL) OR (route_kind='FINANCIAL' AND financial_source_id IS NOT NULL AND bank_listener_id IS NULL))
);
CREATE UNIQUE INDEX email_sender_route_active_unique ON email_sender_route(household_id,sender_address) WHERE active;
INSERT INTO email_sender_route(household_id,sender_address,route_kind,bank_listener_id,active)
SELECT household_id,sender_address,'BANK',id,active FROM bank_email_listener ON CONFLICT DO NOTHING;
CREATE TABLE financial_entity_alias (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    entity_type TEXT NOT NULL CHECK (entity_type IN ('ACCOUNT','WEALTH_ACCOUNT')),
    account_id UUID REFERENCES account(id),
    wealth_account_id UUID REFERENCES wealth_account(id),
    alias TEXT NOT NULL CHECK (length(trim(alias)) BETWEEN 1 AND 160),
    normalized_alias TEXT NOT NULL CHECK (length(trim(normalized_alias)) BETWEEN 1 AND 160),
    source TEXT NOT NULL DEFAULT 'SYSTEM' CHECK (source IN ('SYSTEM','USER','REVIEW_LEARNED')),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((entity_type='ACCOUNT' AND account_id IS NOT NULL AND wealth_account_id IS NULL) OR (entity_type='WEALTH_ACCOUNT' AND wealth_account_id IS NOT NULL AND account_id IS NULL))
);
CREATE UNIQUE INDEX financial_entity_alias_unique ON financial_entity_alias(household_id,entity_type,normalized_alias) WHERE active;
CREATE TABLE financial_email_event (
    source_event_id UUID PRIMARY KEY REFERENCES source_event(id),
    financial_source_id UUID NOT NULL REFERENCES financial_email_source(id),
    observed_sender TEXT NOT NULL,
    message_id TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    email_date TEXT NOT NULL DEFAULT '',
    authentication_results TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE financial_email_observation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_event_id UUID NOT NULL REFERENCES source_event(id),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    kind TEXT NOT NULL CHECK (kind IN ('CASH_MOVEMENT','WEALTH_VALUE','NON_ACTIONABLE','UNKNOWN')),
    facts_json JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPLIED','REVIEW','IGNORED')),
    transaction_id UUID REFERENCES transaction(id),
    wealth_observation_id UUID REFERENCES wealth_observation(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source_event_id,ordinal)
);
CREATE TABLE financial_email_preview (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    financial_source_id UUID NOT NULL REFERENCES financial_email_source(id),
    subject TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 200000),
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PROCESSING','SUCCEEDED','FAILED')),
    result_json JSONB,
    error_message TEXT,
    created_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE wealth_observation ALTER COLUMN document_id DROP NOT NULL;
ALTER TABLE wealth_observation ADD COLUMN financial_email_observation_id UUID REFERENCES financial_email_observation(id);
ALTER TABLE source_event DROP CONSTRAINT IF EXISTS source_event_source_type_check;
ALTER TABLE source_event ADD CONSTRAINT source_event_source_type_check CHECK (source_type IN ('BANK_EMAIL','FINANCIAL_EMAIL','TELEGRAM_TEXT','TELEGRAM_CALLBACK','TELEGRAM_IMAGE','WEB_MANUAL','WEB_IMAGE','SYSTEM'));
ALTER TABLE email_ingress_delivery ADD COLUMN financial_source_id UUID REFERENCES financial_email_source(id);
ALTER TABLE email_ingress_delivery ADD CONSTRAINT email_ingress_delivery_route_check CHECK ((listener_id IS NOT NULL AND financial_source_id IS NULL) OR (listener_id IS NULL AND financial_source_id IS NULL) OR (listener_id IS NULL AND financial_source_id IS NOT NULL));
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_job_lane() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.lane := CASE
    WHEN NEW.type IN ('PROCESS_TELEGRAM_CALLBACK','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','PROCESS_TELEGRAM_REVIEW_TEXT') THEN 'INTERACTIVE'
    WHEN NEW.type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL','PROCESS_FINANCIAL_EMAIL','PROCESS_FINANCIAL_EMAIL_PREVIEW') THEN 'BACKGROUND'
    ELSE 'DEFAULT'
  END;
  RETURN NEW;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_job_lane() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.lane := CASE
    WHEN NEW.type IN ('PROCESS_TELEGRAM_CALLBACK','SEND_TELEGRAM_MESSAGE','EDIT_TELEGRAM_MESSAGE','PROCESS_TELEGRAM_REVIEW_TEXT') THEN 'INTERACTIVE'
    WHEN NEW.type IN ('FETCH_TELEGRAM_IMAGE','FINALIZE_TELEGRAM_MEDIA_GROUP','PROCESS_DOCUMENT','PROCESS_PAYSLIP','PROCESS_RECEIPT','PROCESS_TRANSACTION_SCREENSHOT','GENERATE_INSIGHT','PROCESS_BANK_EMAIL') THEN 'BACKGROUND'
    ELSE 'DEFAULT'
  END;
  RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER TABLE email_ingress_delivery DROP CONSTRAINT email_ingress_delivery_route_check;
ALTER TABLE email_ingress_delivery DROP COLUMN financial_source_id;
ALTER TABLE source_event DROP CONSTRAINT source_event_source_type_check;
ALTER TABLE source_event ADD CONSTRAINT source_event_source_type_check CHECK (source_type IN ('BANK_EMAIL','TELEGRAM_TEXT','TELEGRAM_CALLBACK','TELEGRAM_IMAGE','WEB_MANUAL','WEB_IMAGE','SYSTEM'));
ALTER TABLE wealth_observation DROP COLUMN financial_email_observation_id;
ALTER TABLE wealth_observation ALTER COLUMN document_id SET NOT NULL;
DROP TABLE financial_email_preview;
DROP TABLE financial_email_observation;
DROP TABLE financial_email_event;
DROP INDEX financial_entity_alias_unique;
DROP TABLE financial_entity_alias;
DROP INDEX email_sender_route_active_unique;
DROP TABLE email_sender_route;
DROP INDEX financial_email_source_household_idx;
DROP TABLE financial_email_source;
