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
- [x] Full backend, frontend, container, and secret CI pass.
- [x] Merge and publish immutable images.
- [x] Reclaim merged worktree and disposable caches after image publication.
- [x] Submit production deployment approval.
- [x] Verify `/healthz`, `/readyz`, and active Cloudflare ingestion after deployment. Merged as `b42493a` (migration `00044` applied, `gmail_integration` table gone); 109 deliveries `INGESTED` in production; Deploy Production runs succeed (checked 2026-10-03).
- [x] Revoke obsolete external Google OAuth/PubSub resources separately. Done 2026-10-03 by the owner by deleting the Google Cloud project that held the Pub/Sub topic and subscriptions, the push service account, the OAuth client and the Gmail API configuration (reported by the owner; not independently verifiable from the host, which has no Google credentials). Locally, `google-oauth-client.json` and all `GMAIL_*` settings are already gone.
