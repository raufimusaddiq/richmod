# V4 Canonical Review Ignore Fix

- [x] Reproduced production `400` responses for `UNKNOWN_BANK_TEMPLATE` ignore actions.
- [x] Removed multi-command prepared statements from canonical review resolution.
- [x] Kept source, proposal, document, review item, review delivery, and audit updates in one transaction.
- [x] Added a PostgreSQL-backed regression test for source-only bank reviews.
- [x] Verified `IGNORE` returns `204`, marks the source `IGNORED`, and resolves the review.
- [x] API tests and vet pass.
