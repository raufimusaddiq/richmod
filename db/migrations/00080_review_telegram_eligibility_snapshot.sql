-- +goose Up
ALTER TABLE review_item ADD COLUMN telegram_eligible_at_creation boolean;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION review_item_snapshot_telegram_eligibility() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	NEW.telegram_eligible_at_creation := EXISTS (
		SELECT 1 FROM telegram_identity ti
		JOIN household_member hm ON hm.household_id=ti.household_id AND hm.user_id=ti.user_id AND hm.active
		WHERE ti.household_id=NEW.household_id AND ti.active);
	RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER review_item_snapshot_telegram_eligibility
	BEFORE INSERT ON review_item
	FOR EACH ROW EXECUTE FUNCTION review_item_snapshot_telegram_eligibility();

-- +goose Down
DROP TRIGGER IF EXISTS review_item_snapshot_telegram_eligibility ON review_item;
DROP FUNCTION IF EXISTS review_item_snapshot_telegram_eligibility();
ALTER TABLE review_item DROP COLUMN IF EXISTS telegram_eligible_at_creation;
