-- +goose Up
CREATE TABLE integration_action (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    integration_type TEXT NOT NULL,
    action_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('OPEN','RESOLVED','DISMISSED','EXPIRED')),
    title TEXT NOT NULL,
    description TEXT,
    action_url TEXT,
    action_code TEXT,
    source_delivery_id UUID REFERENCES email_ingress_delivery(id),
    dedupe_key TEXT NOT NULL,
    metadata_json JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    resolved_by_user_id UUID REFERENCES "user"(id)
);

CREATE UNIQUE INDEX integration_action_dedupe_unique
    ON integration_action(household_id,integration_type,action_type,dedupe_key);
CREATE INDEX integration_action_household_status_created_idx
    ON integration_action(household_id,status,created_at DESC);

-- +goose Down
DROP TABLE integration_action;
