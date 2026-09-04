# V4 Gmail listener routing increment

- [x] Exact active listener sender matching
- [x] DKIM and DMARC pass required before extraction is queued
- [x] Unmatched and unauthenticated messages fail closed
- [x] `PROCESS_BANK_EMAIL` is queued in the background lane
- [x] Generic extraction job is idempotent and preserves recoverable source status on failure
- [x] Generic native extraction is the only matched-listener processing path
- [x] Primary mode writes proposal, transaction, evidence, audit, and review records
- [x] Known-account matching is household-scoped and suffix-based
- [x] Existing worker and generic bank-email regression tests pass
- [x] Listener evidence records extraction output and policy result
- [x] Listener configuration is household-scoped and owner-managed
- [x] Live native-tool smoke test against the production `primary` model
