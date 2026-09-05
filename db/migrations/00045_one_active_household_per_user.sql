-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM household_member
        WHERE active = TRUE
        GROUP BY user_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot enforce one active household per user: duplicate active memberships exist';
    END IF;
END
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX household_member_one_active_household_per_user
    ON household_member(user_id)
    WHERE active = TRUE;

-- +goose Down
DROP INDEX IF EXISTS household_member_one_active_household_per_user;
