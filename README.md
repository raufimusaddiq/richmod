# Family Finance

Self-hosted household income and expense tracking. PostgreSQL is the canonical
financial state; Go performs all financial mutations.

## Current phase

The production baseline includes the canonical ledger, authentication, Gmail/Jago
automation, Telegram text intake and review, generic financial-document intake,
reconciliation, deterministic analytics, and aggregate-only LLM insights.

Current delivery follows
[`docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md`](docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md):
household onboarding and financial correctness first, followed by a routed web
experience and a broader finance-scoped Telegram assistant. Budgeting remains a
dormant backend capability, not part of the active product experience. Progress
and verification evidence are maintained in
[`docs/MVP_COMPLETION_CHECKLIST.md`](docs/MVP_COMPLETION_CHECKLIST.md).

OWNER users can manage household members at `/household`. Telegram is linked by
an expiring, single-use bot invitation; the recipient proves ownership by opening
the invite and sending `/start` from their own Telegram account. Raw invitation
tokens are returned only at creation and are stored as hashes.

Authorized household members may send a JPEG/PNG photo or image document directly
to the Telegram bot. It is downloaded asynchronously with strict size and image
validation, normalized into the generic finance-document pipeline, and then
classified as a payslip, receipt, screenshot, transfer proof, invoice, or other
supported document. A caption is context metadata, never mutation authority.

Bank Jago outgoing transfers remain neutral and absent from spending totals until
classified. Masked known-account hints can resolve owned/household destinations
to TRANSFER and investment destinations to non-spending. Unknown destinations are
resolved through the same Review Inbox object on web or Telegram.

Confirming a category applies only to that transaction. A permanent merchant
category rule is created only when the user separately chooses “remember this
merchant” on the web or explicitly confirms the bound Telegram follow-up. OWNERs
can inspect and disable these household-scoped rules at `/settings`.

All finance calendar logic and database sessions operate in `Asia/Jakarta`
(GMT+7), while persisted instants use `TIMESTAMPTZ`.

## First owner bootstrap

Once migrations have run, create the initial owner exactly once. Pass the
password through standard input so it is not recorded in shell history:

```text
printf '%s\n' 'use-a-unique-12-plus-character-password' | docker compose exec -T api /bootstrap --email owner@example.com --name 'Owner Name' --household 'My Household'
```

The command creates the owner and household atomically, seeds Indonesian
categories, and refuses to run again after an owner exists.

Web sessions expire after 24 hours of inactivity. Each authenticated request
renews the expiry by another 24 hours.

## Local startup

1. Copy `.env.example` to `.env` and replace every placeholder with secure values.
2. Run `docker compose up --build`.
3. Check `http://localhost:8080/healthz` and `http://localhost:8080/readyz`.

The compose ports bind to loopback only. Production uses the host-managed Caddy
instance for TLS at `finance.investdx.biz.id`; see the production deployment
runbook. Secrets must be supplied externally.

Every push to `main` and pull request runs Go tests, the Next.js production
build, Compose validation, and all four production container builds.
