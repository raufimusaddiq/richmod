ALTER TABLE "user" ADD COLUMN IF NOT EXISTS password_initialized_at TIMESTAMPTZ;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS is_super_admin BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE "user" u SET password_initialized_at = u.created_at
WHERE password_initialized_at IS NULL AND EXISTS (SELECT 1 FROM household_member hm WHERE hm.user_id=u.id AND hm.role='OWNER');
CREATE TABLE IF NOT EXISTS dashboard_account_invite (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), household_id UUID NOT NULL REFERENCES household(id), user_id UUID NOT NULL REFERENCES "user"(id),
 token_hash BYTEA NOT NULL UNIQUE, status TEXT NOT NULL CHECK(status IN ('PENDING','CONSUMED','REVOKED','EXPIRED')),
 expires_at TIMESTAMPTZ NOT NULL, created_by_user_id UUID NOT NULL REFERENCES "user"(id), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), consumed_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS dashboard_account_invite_pending ON dashboard_account_invite(household_id,user_id) WHERE status='PENDING';
CREATE INDEX IF NOT EXISTS dashboard_account_invite_user_idx ON dashboard_account_invite(user_id,created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS dashboard_account_invite;
ALTER TABLE "user" DROP COLUMN IF EXISTS password_initialized_at;
ALTER TABLE "user" DROP COLUMN IF EXISTS is_super_admin;
-- +goose Up
