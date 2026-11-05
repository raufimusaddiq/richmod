-- +goose Up
-- Identity bindings are 1:1 (ADR-059):
--   Telegram user ID 1:1 user   (telegram_identity primary key + user_id unique)
--   user             1:1 household (household_member user_id unique, active or not)
-- and a Telegram identity always belongs to its own user's membership. A
-- Telegram ID or user is never rebound or moved; removal only deactivates.
-- Creating these fails explicitly if existing data already breaks a binding.
CREATE UNIQUE INDEX household_member_one_household_per_user ON household_member (user_id);
CREATE UNIQUE INDEX telegram_identity_one_per_user ON telegram_identity (user_id);
ALTER TABLE telegram_identity
    ADD CONSTRAINT telegram_identity_membership_fkey
    FOREIGN KEY (household_id, user_id) REFERENCES household_member (household_id, user_id);

-- +goose Down
ALTER TABLE telegram_identity DROP CONSTRAINT IF EXISTS telegram_identity_membership_fkey;
DROP INDEX IF EXISTS telegram_identity_one_per_user;
DROP INDEX IF EXISTS household_member_one_household_per_user;
