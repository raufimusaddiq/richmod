-- +goose Up
CREATE TABLE email_ingress_address (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    local_part TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('BANK_EMAIL')),
    provider TEXT NOT NULL CHECK (provider IN ('CLOUDFLARE_EMAIL')),
    status TEXT NOT NULL CHECK (status IN ('PROVISIONED','ACTIVE','DISABLED')),
    created_by_user_id UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    disabled_at TIMESTAMPTZ,
    last_received_at TIMESTAMPTZ,
    CONSTRAINT email_ingress_local_part_format CHECK (local_part ~ '^h_[a-f0-9]{32}$')
);
CREATE UNIQUE INDEX email_ingress_address_local_part_unique ON email_ingress_address(local_part);
CREATE UNIQUE INDEX email_ingress_address_current_household_unique ON email_ingress_address(household_id,purpose) WHERE status IN ('PROVISIONED','ACTIVE');

CREATE TABLE email_ingress_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    address_id UUID NOT NULL REFERENCES email_ingress_address(id),
    household_id UUID NOT NULL REFERENCES household(id),
    listener_id UUID REFERENCES bank_email_listener(id),
    source_event_id UUID REFERENCES source_event(id),
    provider TEXT NOT NULL CHECK (provider IN ('CLOUDFLARE_EMAIL')),
    object_key TEXT NOT NULL,
    content_sha256 BYTEA NOT NULL,
    raw_size BIGINT,
    envelope_from TEXT,
    observed_sender TEXT,
    internet_message_id TEXT,
    subject TEXT,
    email_date TEXT,
    authentication_results TEXT,
    arc_authentication_results TEXT,
    status TEXT NOT NULL CHECK (status IN ('PROVISIONED_RECEIVED','INGESTED','IGNORED_UNMATCHED','IGNORED_AUTH','DUPLICATE')),
    reason_code TEXT,
    received_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX email_ingress_delivery_object_unique ON email_ingress_delivery(object_key);
CREATE UNIQUE INDEX email_ingress_delivery_payload_unique ON email_ingress_delivery(address_id,content_sha256);

ALTER TABLE bank_email_event RENAME COLUMN gmail_message_id TO message_id;

-- +goose Down
ALTER TABLE bank_email_event RENAME COLUMN message_id TO gmail_message_id;
DROP TABLE email_ingress_delivery;
DROP TABLE email_ingress_address;
