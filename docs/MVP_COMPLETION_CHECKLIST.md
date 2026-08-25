# Family Finance MVP completion checklist

**Audit date:** 2026-08-25

**Repository baseline:** `ff941e7`

**Production:** `https://finance.investdx.biz.id`, schema version 12

**Scope:** household income and expense tracking, IDR only, `Asia/Jakarta`

This is the live implementation checklist. It records repository and production
evidence; roadmap phase names alone are not evidence of completion.

Status key:

- `[x]` implemented and verified;
- `[~]` partially implemented or dependent on an external production choice;
- `[ ]` not implemented or not verified;
- `[-]` deliberately outside the current MVP.

## Scope and invariants

- [x] Household-scoped canonical financial state is stored in PostgreSQL.
- [x] Go owns financial state transitions; handlers never accept LLM output as
  direct mutation authority.
- [x] Income and expense are the only current user-facing ledger flows.
- [x] The ledger accepts whole-rupiah IDR only (`NUMERIC`, never float).
- [x] Household time is interpreted and displayed in `Asia/Jakarta` (GMT+7).
- [x] Source events and evidence are retained during deduplication and review.
- [x] Canonical transactions use lifecycle states and are voided, not deleted.
- [x] Ambiguity is routed to Review Inbox instead of guessed.
- [x] Financial mutations and review decisions are audited and household-scoped.
- [x] Webhooks and PostgreSQL jobs use stable idempotency boundaries.
- [x] Deterministic manual, Jago parsing, and analytics flows work without LLM.
- [x] Bank Jago uses `SPENDING_ONLY`; incoming money is not household income.
- [x] LLM access is through the configured Cloud LLM Gateway only.
- [x] No Redis, message broker, vector database, local model runtime, or asset
  module was introduced.
- [-] Canonical transfer, refund, and adjustment creation workflows are deferred.
  Jago pocket/own-account movement is still recognized so it can be ignored as
  spending.
- [-] Assets, stocks, funds, gold, crypto, broker integrations, valuation, and
  investment P&L remain outside the MVP.

## Acceptance criteria audit

### Foundation

- [x] API, web, worker, PostgreSQL, and migration containers start in Compose.
- [x] Forward-only Goose migrations apply through version 12.
- [x] Public `/healthz` and `/readyz` endpoints work through Caddy.
- [x] Runtime secrets are external to Git; CI includes secret scanning.
- [x] One-time owner bootstrap, Argon2id login, secure sliding 24-hour sessions,
  logout, and membership-backed authorization work.
- [x] API and worker tests, Go vet, frontend build, Compose validation, secret
  scanning, and production image builds run in CI.
- [ ] sqlc is not configured even though it appears in the original development
  order. Current code uses parameterized pgx queries. Resolve through adoption
  or an ADR before calling the original order fully complete.

### Household and settings

- [x] Household, owner user, membership, accounts, categories, merchants, and
  confirmed merchant aliases exist.
- [~] OWNER/MEMBER roles are enforced, but there is no member invitation or
  membership-management flow after bootstrap.
- [~] Account, category, and merchant list/create operations work and are audited.
- [ ] Account/category/merchant update and safe deactivation endpoints are absent.
- [ ] The frontend has no settings/member-management interface.

### Core ledger

- [x] Manual income and expense creation works through the authenticated API.
- [x] Every manual transaction receives a source event, evidence, and audit row.
- [x] Transaction list/detail, evidence, confirm, and void operations exist.
- [x] Money validation requires a positive whole-IDR string.
- [x] Canonical transactions are never hard-deleted.
- [ ] The frontend has no manual income/expense entry form or transaction-detail
  view.

### Bank Jago and Gmail

- [x] Gmail OAuth, encrypted refresh-token storage, Pub/Sub authentication, watch
  renewal, and history processing are implemented and active in production.
- [x] Gmail notifications and Jago messages become idempotent source events/jobs.
- [x] Known authenticated Jago payment templates parse deterministically first.
- [x] Incoming money, pocket movement, and RDN/investment movement do not become
  MVP income or expense.
- [x] Ambiguous external outgoing transfers go to review.
- [x] Sender authentication and parser-policy fixtures exist.
- [~] Failed/stale jobs retry safely, but there is no owner-visible command to
  explicitly reprocess a selected source event after a parser fix.

### Telegram text and review

- [x] Telegram authorization uses numeric Telegram user ID.
- [x] Unauthorized identities receive no household data or identity disclosure.
- [x] Secret-authenticated, private-chat, finance-only text intake works.
- [x] Natural-language IDR income/expense extraction uses a strict schema and Go
  validation; ambiguous output becomes review.
- [x] Review push, stored Telegram message binding, `reply_to_message_id`, exact
  transaction binding, deterministic category selection, audit, and concurrent
  conversation separation are implemented.
- [x] Confirmed merchant/category mappings can be learned only from explicit user
  confirmation.
- [ ] Telegram photo/document intake is not implemented; the webhook currently
  ignores messages without text. Images must currently be uploaded in the web UI.

### Generic documents, payslips, receipts, and screenshots

- [x] Authenticated JPEG/PNG web upload validates size, dimensions, MIME/extension,
  strips metadata through re-encoding, hashes content, and stores opaque evidence.
- [x] One generic classification pipeline covers supported financial documents.
- [x] Payslip extraction validates IDR arithmetic and creates income through Go;
  payroll deductions remain metadata and never become household expenses.
- [x] Receipt extraction validates totals and only enriches a unique strong match;
  unmatched or ambiguous receipts go to review instead of duplicating expense.
- [x] Bank/e-wallet/history/transfer screenshots support multiple independent
  proposals, strict validation, reconciliation, and Jago spending-only handling.
- [x] Bills/invoices are classified but do not become paid transactions merely
  because the document exists.

### Reconciliation and Review Inbox

- [x] Candidate generation and scoring are deterministic and tested.
- [x] Only a unique high-confidence match can auto-link evidence.
- [x] Ambiguous candidates remain visible in Review Inbox.
- [x] Confirm/reject/merge decisions are household-scoped and audited.
- [x] Evidence merge is reversible and original source evidence is retained.

### Dashboard, budgets, analytics, and insights

- [x] Deterministic queries provide monthly income, expense, net cashflow, savings
  rate, category spending, top merchants, six-month cashflow, member contribution,
  and review count.
- [x] The web dashboard displays income, expense, net cashflow, Review Inbox,
  category/merchant/member trends, document intake, budgets, insights, and recent
  transactions.
- [ ] Savings rate is returned by the API but is not displayed by the frontend.
- [~] Transaction provenance/evidence is available through authenticated APIs but
  is not exposed in a transaction-detail frontend view.
- [x] Recurring monthly whole-IDR category budgets include descendant categories,
  net refunds in analytics, and can be deactivated by an owner.
- [x] LLM insights receive aggregate metrics only, are non-authoritative, enforce
  completeness thresholds, and use model route `primary` in production.
- [~] The intended multi-page navigation is represented as one functional dashboard
  rather than separate Overview/Transactions/Spending/Settings pages.

### Security regression matrix

- [x] Unauthorized Telegram identity and forged Telegram webhook tests.
- [x] Authenticated Gmail Pub/Sub audience/service-account/mailbox tests.
- [x] Secure cookie, Argon2id, request security headers, request correlation,
  same-origin mutation, and rate-limit tests.
- [x] Invalid MIME/extension, metadata stripping, and document dimension tests.
- [x] Strict LLM schemas reject missing/unknown fields and invalid IDR values.
- [~] Oversized request/upload limits exist, but an explicit regression test for
  the oversized-upload response is missing.
- [~] Parameterized SQL and household filters are used, but dedicated SQL-injection
  and cross-household integration tests are missing.
- [~] Prompts identify document/email/user text as untrusted, but dedicated prompt-
  injection fixtures for receipts, email, and merchant names are missing.
- [~] CSP and escaped React rendering are active, but an explicit XSS regression
  fixture is missing.
- [~] Webhook/source idempotency constraints exist, but a replay integration test
  proving one canonical result is missing.
- [ ] Malicious filename/path traversal, secret-redaction, and fake-sender end-to-
  end regression cases are not all represented in the automated suite.

### Production operations

- [x] Public TLS is managed by Caddy at `finance.investdx.biz.id`.
- [x] PostgreSQL, API, worker, and web expose no public host ports.
- [x] HSTS, security headers, request-body limits, CSRF origin checks, login/webhook
  rate limits, and framework/server header suppression are active.
- [x] Services use restart policies, process/memory limits, and
  `no-new-privileges` where compatible.
- [x] Structured API/worker logs, request IDs, readiness, worker heartbeat, job
  failure/backlog, Review backlog, and Gmail freshness are available.
- [x] Daily encrypted restic backup is enabled at 02:30 `Asia/Jakarta` with
  14 daily, 8 weekly, and 12 monthly retention.
- [x] A production snapshot passed archive verification, restic integrity check,
  isolated PostgreSQL restore, and canonical/restored financial count comparison
  on 2026-08-25.
- [~] The active repository is still a Docker volume on the application host.
  Off-host storage is required to survive loss or compromise of the server.
- [~] Owner-visible operational status exists, but there is no external alert
  delivery for failed jobs, stale heartbeat, Gmail staleness, or backup failure.

## Roadmap phase status

| Phase | Status | Remaining work |
|---|---|---|
| 0 — Bootstrap | `[~]` | Resolve the sqlc deviation. |
| 1 — Core Ledger | `[~]` | Settings lifecycle, member management, and frontend manual entry/detail. |
| 2 — Minimal Dashboard | `[x]` | Acceptance met; later UI gaps are tracked under Phase 10. |
| 3 — Telegram Manual Input | `[~]` | Telegram photo/document intake. |
| 4 — Bank Jago Automation | `[~]` | Explicit source-event reprocessing control. |
| 5 — Reconciliation | `[x]` | Acceptance met. |
| 6 — Telegram Interactive Review | `[x]` | Acceptance met for text-based review. |
| 7 — Generic Image Intake | `[x]` | Web intake complete; Telegram transport remains Phase 3 work. |
| 8 — Payslip | `[x]` | Acceptance met. |
| 9 — Receipt / Screenshot | `[x]` | Acceptance met. |
| 10 — Budgets and Analytics | `[~]` | Savings-rate card, provenance/detail, settings/manual-entry UI. |
| 11 — LLM Insights | `[x]` | Acceptance met; aggregate-only boundary documented in ADR-015. |
| 12 — Production Hardening | `[~]` | Off-host backup, external alerts, and missing security regression cases. |

## Fix order before declaring the MVP complete

1. Add the missing security and household-isolation regression tests.
2. Add Telegram photo intake into the existing generic document pipeline.
3. Add owner-controlled source-event reprocessing with an audit trail.
4. Complete safe settings lifecycle and household member management.
5. Add frontend manual entry, transaction evidence/provenance detail, settings,
   and the savings-rate card.
6. Configure external alert delivery for operational failures.
7. Configure and restore-test the encrypted off-host repository.
8. Resolve the sqlc roadmap deviation through implementation or an ADR.

The MVP is not considered complete while any P0 or production item above remains
`[ ]` or `[~]`. Optional UI refinement may continue after those gaps close.
