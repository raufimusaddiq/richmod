-- +goose Up
-- PRD §7 canonical ReviewDecision contract. One additive column holds the
-- structured decision that explains why a review exists: what Richmod knows,
-- proposes, is missing, and cannot reconcile. The column is nullable so
-- existing open reviews stay resolvable and are not backfilled with invented
-- facts (PRD §30). Nothing here changes confirmation policy.
ALTER TABLE review_item ADD COLUMN decision jsonb;

-- +goose Down
ALTER TABLE review_item DROP COLUMN decision;
