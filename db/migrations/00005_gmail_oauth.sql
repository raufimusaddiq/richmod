-- +goose Up
CREATE TABLE gmail_oauth_state (
    state_hash BYTEA PRIMARY KEY,
    household_id UUID NOT NULL REFERENCES household(id),
    user_id UUID NOT NULL REFERENCES "user"(id),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gmail_integration (
    household_id UUID PRIMARY KEY REFERENCES household(id),
    mailbox TEXT NOT NULL,
    encrypted_refresh_token BYTEA NOT NULL,
    granted_scope TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('CONNECTED', 'WATCH_ACTIVE', 'ERROR', 'DISCONNECTED')),
    connected_by_user_id UUID NOT NULL REFERENCES "user"(id),
    watch_expiration TIMESTAMPTZ,
    history_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE gmail_integration;
DROP TABLE gmail_oauth_state;
