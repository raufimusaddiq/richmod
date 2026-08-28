# V4 deploy-readiness checklist

This checklist is separate from the V4 iteration document. It records the
current deploy gate without changing historical iteration documentation.

- [x] Generic-primary is the default for matched bank listeners.
- [x] Legacy Jago processing cannot run alongside generic-primary.
- [x] Existing legacy Gmail sender is seeded idempotently at worker startup.
- [x] Seeded accounts are system-managed and household-scoped.
- [x] Seeded listeners are audited.
- [x] Worker full Go test suite passes.
- [x] API full Go test suite passes.
- [x] Worker, API, and web images build successfully.
- [x] Compose configuration validates with required environment variables.
- [x] Health and readiness endpoints respond successfully.
- [x] Disposable PostgreSQL rehearsal applies migrations through version 26.
- [ ] Production database migration 00026 is applied after backup.
- [ ] New worker/API/web images are deployed and restarted.
- [ ] Seeded listener is verified in the production database.
- [ ] One controlled Jago message completes generic extraction and audit verification.
- [ ] Direct authenticated Jenius message is observed before enabling Jenius.
- [ ] Telegram multi-recipient live verification passes.
