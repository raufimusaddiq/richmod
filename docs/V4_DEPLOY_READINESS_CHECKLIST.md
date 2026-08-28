# V4 deploy-readiness checklist

This checklist is separate from the V4 iteration document. It records the
current deploy gate without changing historical iteration documentation.

- [x] Generic extraction is the only path for matched bank listeners.
- [x] Generic-only processing is used for every matched listener.
- [x] No legacy sender configuration or bank-specific listener seed is required.
- [x] Listener-associated accounts are system-managed and household-scoped.
- [x] Listener mutations are audited.
- [x] Worker full Go test suite passes.
- [x] API full Go test suite passes.
- [x] Worker, API, and web images build successfully.
- [x] Compose configuration validates with required environment variables.
- [x] Health and readiness endpoints respond successfully.
- [x] Disposable PostgreSQL rehearsal applies migrations through version 26.
- [x] Production database migration 00026 is applied after backup.
- [x] New worker/API/web images are deployed and restarted.
- [x] Configured listener is verified in the production database.
- [x] Existing spending bank accounts are reused by matching listeners; empty migration duplicates are deactivated without deleting transaction history.
- [ ] One controlled configured-bank message completes generic extraction and audit verification.
- [ ] A second configured-bank sender is verified through the same generic path.
- [x] Telegram multi-recipient live verification passes; production bank-review runs delivered the same review to both active linked identities and a member callback resolved the shared review once.
# Current remediation note (2026-08-29)

Application-side queue isolation, callback ingress, single-protocol gateway
behavior, bank outcome persistence, and multipage document contracts are covered
by automated/local verification. Real second-sender bank acceptance and off-host
backup verification remain pending production acceptance items.
