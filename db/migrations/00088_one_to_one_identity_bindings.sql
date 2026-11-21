-- +goose Up
CREATE UNIQUE INDEX household_member_one_household_per_user ON household_member (user_id);
CREATE UNIQUE INDEX telegram_identity_one_per_user ON telegram_identity (user_id);
ALTER TABLE telegram_identity
    ADD CONSTRAINT telegram_identity_membership_fkey
    FOREIGN KEY (household_id, user_id) REFERENCES household_member (household_id, user_id);

-- +goose Down
ALTER TABLE telegram_identity DROP CONSTRAINT IF EXISTS telegram_identity_membership_fkey;
DROP INDEX IF EXISTS telegram_identity_one_per_user;
DROP INDEX IF EXISTS household_member_one_household_per_user;
