-- +goose Up
-- Reuse an existing spending bank account when a listener migration created an
-- empty system-managed duplicate. Historical transactions remain attached to
-- the existing account; only listener linkage and duplicate account state move.
CREATE TEMP TABLE v4_bank_listener_account_merge ON COMMIT DROP AS
SELECT l.id AS listener_id,
       l.account_id AS duplicate_account_id,
       candidate.id AS existing_account_id,
       l.sender_address
FROM bank_email_listener l
JOIN account duplicate_account ON duplicate_account.id = l.account_id
    AND duplicate_account.system_managed
JOIN LATERAL (
    SELECT a.id
    FROM account a
    WHERE a.household_id = l.household_id
      AND a.id <> duplicate_account.id
      AND a.account_type = 'BANK'
      AND a.tracking_policy = 'SPENDING_ONLY'
      AND a.active
      AND lower(a.name) IN (lower(l.bank_name), lower('Bank · ' || l.bank_name))
      AND NOT EXISTS (
          SELECT 1 FROM bank_email_listener other
          WHERE other.account_id = a.id AND other.id <> l.id AND other.active
      )
    ORDER BY a.system_managed DESC, a.created_at, a.id
    LIMIT 1
) candidate ON TRUE
WHERE l.active;

UPDATE account a
SET system_key = NULL, updated_at = now()
FROM v4_bank_listener_account_merge m
WHERE a.id = m.duplicate_account_id;

UPDATE bank_email_listener l
SET account_id = m.existing_account_id, updated_at = now()
FROM v4_bank_listener_account_merge m
WHERE l.id = m.listener_id;

UPDATE account a
SET system_managed = true,
    system_key = 'bank-email:' || m.sender_address,
    updated_at = now()
FROM v4_bank_listener_account_merge m
WHERE a.id = m.existing_account_id;

UPDATE account a
SET active = false, updated_at = now()
FROM v4_bank_listener_account_merge m
WHERE a.id = m.duplicate_account_id
  AND NOT EXISTS (SELECT 1 FROM transaction t WHERE t.account_id = a.id);

-- +goose Down
-- Account consolidation is intentionally not reversed: restoring duplicate
-- accounts would risk breaking canonical transaction attribution.
