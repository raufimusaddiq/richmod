-- +goose Up
ALTER TABLE review_item ADD COLUMN decision jsonb;

-- +goose Down
ALTER TABLE review_item DROP COLUMN decision;
