-- +goose Up
CREATE TABLE IF NOT EXISTS platform_audit_log (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id UUID NOT NULL REFERENCES "user"(id),
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS platform_audit_log_created_idx ON platform_audit_log(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS platform_audit_log_actor_time_idx ON platform_audit_log(actor_user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS platform_audit_log_entity_idx ON platform_audit_log(entity_type, entity_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS platform_audit_log;
