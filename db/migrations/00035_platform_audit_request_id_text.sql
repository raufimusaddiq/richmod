-- +goose Up
ALTER TABLE platform_audit_log
  ALTER COLUMN request_id TYPE TEXT
  USING request_id::text;

-- +goose Down
ALTER TABLE platform_audit_log
  ALTER COLUMN request_id TYPE UUID
  USING CASE WHEN request_id ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN request_id::uuid ELSE NULL END;
