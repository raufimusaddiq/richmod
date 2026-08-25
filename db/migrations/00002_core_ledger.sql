-- +goose Up
CREATE TABLE household (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    timezone TEXT NOT NULL DEFAULT 'Asia/Jakarta' CHECK (timezone = 'Asia/Jakarta'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE "user" (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    display_name TEXT NOT NULL CHECK (length(trim(display_name)) BETWEEN 1 AND 120),
    password_hash TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_email_normalized CHECK (email = lower(trim(email)))
);
CREATE UNIQUE INDEX user_email_unique ON "user" (email);

CREATE TABLE household_member (
    household_id UUID NOT NULL REFERENCES household(id),
    user_id UUID NOT NULL REFERENCES "user"(id),
    role TEXT NOT NULL CHECK (role IN ('OWNER', 'MEMBER')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, user_id)
);

CREATE TABLE session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id),
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);
CREATE INDEX session_active_user_idx ON session (user_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE account (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    account_type TEXT NOT NULL CHECK (account_type IN ('BANK', 'CASH', 'EWALLET', 'OTHER')),
    tracking_policy TEXT NOT NULL CHECK (tracking_policy IN ('FULL_LEDGER', 'SPENDING_ONLY', 'REFERENCE_ONLY')),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id, name)
);

CREATE TABLE category (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    parent_id UUID REFERENCES category(id),
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    slug TEXT NOT NULL CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX category_household_idx ON category (household_id, sort_order, name);
CREATE UNIQUE INDEX category_root_slug_unique ON category (household_id, slug) WHERE parent_id IS NULL;
CREATE UNIQUE INDEX category_child_slug_unique ON category (household_id, parent_id, slug) WHERE parent_id IS NOT NULL;

CREATE TABLE merchant (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    normalized_name TEXT NOT NULL CHECK (length(trim(normalized_name)) BETWEEN 1 AND 160),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id, normalized_name)
);

CREATE TABLE merchant_alias (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    raw_name TEXT NOT NULL CHECK (length(trim(raw_name)) BETWEEN 1 AND 160),
    normalized_merchant_id UUID NOT NULL REFERENCES merchant(id),
    default_category_id UUID REFERENCES category(id),
    auto_apply BOOLEAN NOT NULL DEFAULT FALSE,
    created_from_user_confirmation BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (household_id, raw_name)
);

CREATE TABLE source_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    source_type TEXT NOT NULL CHECK (source_type IN ('BANK_EMAIL', 'TELEGRAM_TEXT', 'TELEGRAM_IMAGE', 'WEB_MANUAL', 'WEB_IMAGE', 'SYSTEM')),
    external_id TEXT,
    received_at TIMESTAMPTZ NOT NULL,
    raw_payload_ref TEXT,
    payload_hash BYTEA NOT NULL,
    processing_status TEXT NOT NULL CHECK (processing_status IN ('RECEIVED', 'PROCESSING', 'PROCESSED', 'IGNORED', 'FAILED', 'NEEDS_REVIEW')),
    parser_name TEXT,
    parser_version TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX source_event_external_id_unique ON source_event (household_id, source_type, external_id) WHERE external_id IS NOT NULL;
CREATE UNIQUE INDEX source_event_payload_unique ON source_event (household_id, source_type, payload_hash);

CREATE TABLE transaction (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    account_id UUID REFERENCES account(id),
    type TEXT NOT NULL CHECK (type IN ('INCOME', 'EXPENSE', 'TRANSFER', 'REFUND', 'ADJUSTMENT')),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'CONFIRMED', 'NEEDS_REVIEW', 'VOIDED')),
    amount NUMERIC(20,0) NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL DEFAULT 'IDR' CHECK (currency = 'IDR'),
    transaction_at TIMESTAMPTZ NOT NULL,
    merchant_id UUID REFERENCES merchant(id),
    category_id UUID REFERENCES category(id),
    description TEXT,
    note TEXT,
    counterparty_name TEXT,
    external_reference TEXT,
    source_confidence NUMERIC(5,4) CHECK (source_confidence >= 0 AND source_confidence <= 1),
    classification_confidence NUMERIC(5,4) CHECK (classification_confidence >= 0 AND classification_confidence <= 1),
    created_by_user_id UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ,
    voided_at TIMESTAMPTZ,
    CONSTRAINT transaction_confirmed_timestamp CHECK ((status = 'CONFIRMED') = (confirmed_at IS NOT NULL)),
    CONSTRAINT transaction_voided_timestamp CHECK ((status = 'VOIDED') = (voided_at IS NOT NULL))
);
CREATE INDEX transaction_household_time_idx ON transaction (household_id, transaction_at DESC);
CREATE INDEX transaction_household_status_idx ON transaction (household_id, status, transaction_at DESC);

CREATE TABLE transaction_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES transaction(id),
    source_event_id UUID NOT NULL REFERENCES source_event(id),
    evidence_type TEXT NOT NULL,
    confidence NUMERIC(5,4) CHECK (confidence >= 0 AND confidence <= 1),
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transaction_id, source_event_id)
);
CREATE INDEX transaction_evidence_source_idx ON transaction_evidence (source_event_id);

CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    actor_type TEXT NOT NULL CHECK (actor_type IN ('USER', 'SYSTEM', 'EMAIL_PARSER', 'LLM_SUGGESTION', 'WORKER', 'TELEGRAM')),
    actor_id UUID,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    before_json JSONB,
    after_json JSONB,
    request_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_entity_idx ON audit_log (household_id, entity_type, entity_id, created_at DESC);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE transaction_evidence;
DROP TABLE transaction;
DROP TABLE source_event;
DROP TABLE merchant_alias;
DROP TABLE merchant;
DROP TABLE category;
DROP TABLE account;
DROP TABLE session;
DROP TABLE household_member;
DROP TABLE "user";
DROP TABLE household;
