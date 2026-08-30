-- +goose Up
-- Legacy transaction review routes resolved the transaction and optional Telegram
-- delivery projection but could leave the canonical review item active.
UPDATE review_item ri
SET status='RESOLVED',
    resolved_at=now(),
    resolution_action='LEGACY_TRANSACTION_RESOLVED',
    resolution_values=jsonb_build_object('transaction_status',t.status),
    updated_at=now()
FROM transaction t
WHERE ri.transaction_id=t.id
  AND ri.status IN ('PENDING_SEND','OPEN')
  AND t.status IN ('CONFIRMED','VOIDED');

-- +goose Down
-- Canonical review resolution is irreversible. Do not reopen items during rollback.
