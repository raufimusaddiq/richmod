-- +goose Up
-- Sprint 4: household-authored cycle decisions. These are human notes, not
-- transactions or Wealth observations, and they never mutate canonical state.
CREATE TABLE cycle_decision(
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES household(id),
    cycle_start date NOT NULL,
    body text NOT NULL CHECK (btrim(body) <> '' AND length(body) <= 2000),
    created_by_user_id uuid NOT NULL REFERENCES "user"(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX cycle_decision_scope_idx ON cycle_decision(household_id, cycle_start, created_at DESC);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM cycle_decision) THEN
        RAISE EXCEPTION 'cannot roll back while household cycle decisions exist';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE cycle_decision;
