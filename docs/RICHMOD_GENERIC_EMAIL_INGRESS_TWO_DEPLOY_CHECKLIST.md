# Generic email ingress — implementation checklist

Source of truth: `docs/RICHMOD_GENERIC_EMAIL_INGRESS_TWO_DEPLOY_CODEX.md`.
Do not mark an operational item from code inspection alone.

## Deploy 1 — code and migration

- [x] New additive migration creates `email_ingress_address`.
- [x] New additive migration creates `email_ingress_delivery`.
- [x] `bank_email_event.gmail_message_id` renamed to `message_id` in a new migration; historical values preserved.
- [x] Generated address uses 16 cryptographically random bytes and `h_<32 lowercase hex>@richmod.link`.
- [x] Current-address uniqueness is household/purpose scoped.
- [x] HMAC validates required headers, timestamp ±5 minutes, body SHA-256, and `hmac.Equal`.
- [x] HMAC secret is not stored in DB or logs.
- [x] MIME parser handles `text/plain`, `text/html`, multipart alternatives, base64, quoted-printable, and RFC2047 subject decoding.
- [x] Raw body is bounded before parsing.
- [x] Recipient domain and format are validated.
- [x] Recipient resolves household before sender/listener lookup.
- [x] Sender matching is exact, lowercased, active-listener, and household-scoped.
- [x] Trusted auth-service IDs are configuration-driven; no institution branch exists.
- [x] `PROVISIONED` records delivery only and creates no financial source/job.
- [x] `ACTIVE` creates delivery, source metadata, bank event, and `PROCESS_BANK_EMAIL` in one DB transaction.
- [x] Duplicate delivery is idempotent.
- [x] Unknown/disabled recipients do not disclose existence or create financial state.
- [x] Owner-only provision, activate, and rotate endpoints exist.
- [x] Activation only requires a locked `PROVISIONED` address; missing trusted-auth configuration still fails closed in ACTIVE delivery.
- [x] Activation atomically disconnects Gmail.
- [x] Gmail OAuth cannot reconnect after Cloudflare activation.
- [x] Gmail renewal/history paths no-op after disconnect and cannot mutate after cutover.
- [x] Settings UI exposes provision/activate/rotate state.
- [x] Cloudflare ingress and delivery Workers are included without secrets.
- [x] ADR-033 added.

## Deploy 1 — automated verification

- [x] HMAC unit tests cover valid, tampered body, wrong secret, and auth verdicts.
- [x] MIME unit tests cover multipart and visible HTML text.
- [x] API, Gmail, and bank-email package tests pass in Go 1.24.1 container.
- [x] Disposable PostgreSQL migration and ingress integration test passes.
- [x] Cross-household recipient isolation test passes against disposable PostgreSQL.

## Deploy 1 — production evidence (must be observed, not assumed)

- [ ] Migration applied successfully in production.
- [ ] API and worker deployed from merged `main` with unchanged required env.
- [x] Cloudflare R2 bucket, Queue, ingress Worker, and delivery Worker are provisioned manually (operator-provided; verify during rollout).
- [ ] Optional Queue DLQ enabled.
- [x] `*@richmod.link` Email Routing catch-all is provisioned manually (operator-provided; verify during rollout).
- [ ] Household recipient generated and forwarding verification completed.
- [ ] One PROVISIONED delivery visible in R2, Queue, API delivery table, and no source/job.
- [ ] Real forwarded `.eml` inspected; exact `authserv-id` and DKIM/DMARC/ARC policy documented.
- [ ] Trusted authentication IDs configured from captured real `.eml` evidence.
- [ ] Address activated; Gmail status is `DISCONNECTED` in same committed state.
- [ ] One real ACTIVE financial email reaches `PROCESS_BANK_EMAIL` and existing review/ledger flow.
- [ ] Same delivery retried; no duplicate source, job, proposal, or transaction.
- [ ] Late Gmail history job terminally succeeds without ingestion.

## Deploy 2 — only after every required Deploy 1 production item is checked

The optional Queue DLQ is explicitly excluded from this gate.

- [ ] Gmail API routes and runtime files removed.
- [ ] Gmail worker files, maintenance, job decoding, and Gmail job types removed.
- [ ] Gmail env/config removed from code, compose, examples, deployment docs, and production.
- [ ] Gmail jobs terminalized using repository convention.
- [ ] New migration drops `gmail_oauth_state` and `gmail_integration` only after migration audit.
- [ ] `go mod tidy` removes Google dependencies unused elsewhere.
- [ ] Full Go tests/vet and `git diff --check` pass.
- [ ] Cloudflare-only application ingestion verified after Deploy 2.
- [ ] Google OAuth revoked, Pub/Sub removed, and OAuth material removed operationally.

## Explicit non-claims

- [ ] No real Cloudflare or Google resource is claimed until external evidence exists.
- [ ] No Deploy 2 work is marked complete before the Deploy 1 production gate.
