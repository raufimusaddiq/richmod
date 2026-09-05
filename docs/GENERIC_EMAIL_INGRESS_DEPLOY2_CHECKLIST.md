# Generic email ingress Deploy 2 checklist

Source of truth: ADR-033 and the current repository state.

- [x] Production ACTIVE Cloudflare delivery reached `PROCESS_BANK_EMAIL` and a confirmed transaction.
- [x] Remove Gmail OAuth, callback, and Pub/Sub API routes.
- [x] Remove Gmail history/watch worker and job dispatch.
- [x] Remove Gmail configuration from application config and Compose.
- [x] Add a new migration to terminalize obsolete jobs and drop Gmail-only runtime tables.
- [x] Preserve historical financial source events, bank-email events, evidence, proposals, and transactions.
- [x] Remove unused Google dependencies with `go mod tidy`.
- [x] Keep `/finance/v1/email/inbond` and generic `PROCESS_BANK_EMAIL` unchanged.
- [ ] Full backend, frontend, container, and secret CI pass.
- [ ] Merge and publish immutable images.
- [ ] Reclaim merged worktree and disposable caches after image publication.
- [ ] Submit production deployment approval.
- [ ] Verify `/healthz`, `/readyz`, and active Cloudflare ingestion after deployment.
- [ ] Revoke obsolete external Google OAuth/PubSub resources separately.
