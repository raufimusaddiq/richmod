-- +goose Up
ALTER TABLE "user" ADD COLUMN google_sub TEXT;
ALTER TABLE "user" ADD COLUMN google_linked_at TIMESTAMPTZ;
ALTER TABLE "user" ADD CONSTRAINT user_google_sub_nonempty CHECK (google_sub IS NULL OR btrim(google_sub) <> '');
ALTER TABLE "user" ADD CONSTRAINT user_google_sub_linked_at_paired CHECK ((google_sub IS NULL) = (google_linked_at IS NULL));
CREATE UNIQUE INDEX user_google_sub_unique ON "user" (google_sub) WHERE google_sub IS NOT NULL;
CREATE UNIQUE INDEX household_member_one_active_household_per_user ON household_member (user_id) WHERE active = TRUE;
DROP INDEX household_member_one_household_per_user;

-- +goose Down
CREATE UNIQUE INDEX household_member_one_household_per_user ON household_member (user_id);
DROP INDEX IF EXISTS household_member_one_active_household_per_user;
DROP INDEX IF EXISTS user_google_sub_unique;
ALTER TABLE "user" DROP CONSTRAINT IF EXISTS user_google_sub_linked_at_paired;
ALTER TABLE "user" DROP CONSTRAINT IF EXISTS user_google_sub_nonempty;
ALTER TABLE "user" DROP COLUMN IF EXISTS google_linked_at;
ALTER TABLE "user" DROP COLUMN IF EXISTS google_sub;
