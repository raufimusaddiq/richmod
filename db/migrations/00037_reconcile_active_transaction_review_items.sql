-- +goose Up
UPDATE review_item ri
SET status='RESOLVED',
    resolved_at=now(),
    resolution_action='RECONCILED_TERMINAL_TRANSACTION',
    resolution_values=jsonb_build_object('transaction_status',t.status),
    updated_at=now()
FROM transaction t
WHERE ri.transaction_id=t.id
  AND ri.status IN ('PENDING_SEND','OPEN')
  AND t.status IN ('CONFIRMED','VOIDED');

-- +goose Down
