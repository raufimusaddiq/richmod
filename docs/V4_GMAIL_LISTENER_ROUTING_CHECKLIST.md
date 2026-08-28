# V4 Gmail listener routing increment

- [x] Exact active listener sender matching
- [x] DKIM and DMARC pass required before extraction is queued
- [x] Unmatched and unauthenticated messages fail closed
- [x] `PROCESS_BANK_EMAIL` is queued in the background lane
- [x] Shadow job is idempotent and preserves legacy source status on failure
- [x] `GMAIL_GENERIC_PRIMARY=false` remains the safe default
- [x] Primary mode writes proposal, transaction, evidence, audit, and review records
- [x] Known-account matching is household-scoped and suffix-based
- [x] Existing worker and Jago regression tests pass
- [x] Jago-v1 shadow comparison records amount, direction, channel, time, merchant, and policy agreement
- [ ] Jago-v1 promotion evidence and generic-primary rollout
- [ ] Live native-tool smoke test against `ocg/minimax-m3`
