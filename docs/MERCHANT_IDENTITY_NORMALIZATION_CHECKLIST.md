# Merchant Identity Normalization Checklist

Baseline: `main` at `c497f39`.

- [x] Confirm case-only duplicate merchant rows in production data.
- [x] Keep LLM native-tool extraction unchanged; `merchant` already carries detected text.
- [x] Normalize merchant identity at every API/worker persistence boundary.
- [x] Treat case and repeated whitespace as the same household merchant identity.
- [x] Add migration to repoint transactions and aliases before merging duplicate merchant rows.
- [x] Preserve source events, transaction evidence, transactions, and audit rows.
- [x] Add database uniqueness enforcement for canonical merchant identity.
- [x] Prefer a human-readable mixed-case stored name when merging existing duplicates.
- [x] Verify migration through a disposable PostgreSQL 17 database with duplicate fixtures.
- [x] Run complete API and worker tests/vet; run the merchant identity integration test against migration 41.
- [ ] Commit, merge, and deploy only after explicit approval.
