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
- [ ] Production database migration 00026 is applied after backup.
- [ ] New worker/API/web images are deployed and restarted.
- [ ] Configured listener is verified in the production database.
- [ ] One controlled configured-bank message completes generic extraction and audit verification.
- [ ] A second configured-bank sender is verified through the same generic path.
- [ ] Telegram multi-recipient live verification passes.
