-- +goose Up
ALTER TABLE household_member
    ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN deactivated_at TIMESTAMPTZ;

CREATE TABLE telegram_link_invite (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    user_id UUID NOT NULL REFERENCES "user"(id),
    token_hash BYTEA NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','CONSUMED','REVOKED','EXPIRED')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_by_user_id UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX telegram_link_invite_member_idx
    ON telegram_link_invite(household_id,user_id,created_at DESC);
CREATE UNIQUE INDEX telegram_link_invite_pending_member_unique
    ON telegram_link_invite(household_id,user_id) WHERE status='PENDING';

-- +goose Down
DROP TABLE telegram_link_invite;
ALTER TABLE household_member DROP COLUMN deactivated_at, DROP COLUMN active;
