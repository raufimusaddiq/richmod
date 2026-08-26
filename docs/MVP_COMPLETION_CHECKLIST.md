# Richmod Product Alignment v2 implementation checklist

**Alignment date:** 2026-08-25

**Governing document:** [`RICHMOD_PRODUCT_ALIGNMENT_V2.md`](RICHMOD_PRODUCT_ALIGNMENT_V2.md)

**Production:** `https://finance.investdx.biz.id`, schema version 14
**Scope:** household income and expense tracking, IDR only, `Asia/Jakarta`

This is the living delivery checklist for Product Alignment v2. A backend
endpoint alone does not complete a user-facing feature.

- `[x]` implemented and verified
- `[~]` partially implemented
- `[ ]` not implemented or not verified
- `[-]` outside the current iteration

## Product invariants

- [x] PostgreSQL is canonical; Go owns financial state transitions and validates
  all untrusted LLM output.
- [x] Evidence is retained, financial records are never hard-deleted, and
  ambiguity goes to Review Inbox.
- [x] Mutations are audited and household-scoped; webhooks/jobs are idempotent.
- [x] Deterministic flows continue without the LLM gateway.
- [x] Money is whole-rupiah IDR backed by `NUMERIC`; finance time uses
  `Asia/Jakarta` and `TIMESTAMPTZ`.
- [x] Jago is `SPENDING_ONLY`; incoming funding is not household income.
- [x] Telegram authorization and review binding use deterministic numeric/message
  identities.
- [-] Assets, market data, payment execution, and general-purpose assistant
  behavior are outside the iteration.

## Existing production baseline

- [x] Owner bootstrap, Argon2id auth, sliding 24-hour sessions, and roles.
- [x] Canonical ledger, evidence, lifecycle, audit, reconciliation, and Review APIs.
- [x] Gmail/Jago ingestion with sender authentication and idempotency.
- [x] Gmail Pub/Sub accepts authenticated wrapped and unwrapped push delivery.
- [x] Telegram text transaction intake and bound review replies.
- [x] Generic web and Telegram document intake for payslips, receipts,
  screenshots, transfer proof, and invoices.
- [x] Deterministic analytics and aggregate-only insights using model `primary`.
- [x] Caddy TLS/security, private ports, health, logs, limits, local encrypted
  backups, and security regression coverage.
- [~] Backups passed an isolated restore drill but have no off-host replica. This
  is operational hardening, not an Alignment v2 product blocker.

## P0 — Correctness and household onboarding

### A. Household member management

- [x] Household and member list APIs expose roles and Telegram connection state.
- [x] OWNER can add and safely deactivate a MEMBER; non-owners cannot.
- [x] Operations are household-scoped, audited, and preserve history.
- [x] `/household` renders members, roles, connections, and owner actions.

Tests: [x] owner adds member; [x] non-owner rejected; [x] cross-household access
rejected; [x] deactivation preserves attribution; [~] member state production smoke.

### B. Secure Telegram self-link invite

- [x] Add `telegram_link_invite` with token hash only, household/member scope,
  expiry, revocation, single use, and audited state transitions.
- [x] OWNER can generate/revoke an invite; `/start <token>` binds the actual
  sender's numeric Telegram ID to the intended member.
- [x] Expired, consumed, reused, revoked, and cross-household tokens are rejected;
  raw tokens are never logged.
- [x] Spouse onboarding requires no manual Telegram ID discovery.

Tests: [x] expiry; [x] single use/reuse; [x] revocation; [x] household isolation;
[x] correct sender binding; [x] duplicate update idempotency.

### C. Telegram image intake

- [x] Supported Telegram images create one `TELEGRAM_IMAGE` source event and job.
- [x] Authenticated bounded file retrieval passes through the same MIME, size,
  dimension, metadata, storage, and validation controls as web uploads.
- [x] Images enter the existing generic document pipeline, not a parallel pipeline.
- [x] Payslips can yield validated income; receipts enrich a unique match or go to
  review; unsupported/ambiguous documents fail safely.
- [x] Unauthorized senders receive no household/document data.

Tests: [x] authorized/unauthorized image; [x] payslip; [x] receipt; [x] invalid
MIME; [x] oversized image; [x] duplicate update idempotency.

### D. Correct Jago outgoing-transfer semantics

- [x] Incoming Jago, pocket, and RDN/investment movement are excluded.
- [x] Unknown outgoing transfers use the neutral
  `EXPENSE | TRANSFER | IGNORE` resolution flow.
- [x] Unresolved outgoing transfer is never an EXPENSE and never counts as spend.
- [x] Add household-scoped `known_account` with `OWN_ACCOUNT`,
  `HOUSEHOLD_ACCOUNT`, `INVESTMENT_ACCOUNT`, or `OTHER` relationship.
- [x] Own/household destinations resolve to TRANSFER; investment remains excluded;
  unknown external recipients require explicit classification.
- [x] Telegram and web review resolve the same review object; analytics include
  only confirmed financial state and exclude transfers/unresolved proposals.

Tests: [x] unknown transfer unresolved/non-spending; [x] own transfer; [x]
household transfer; [x] external expense; [x] incoming non-income; [x] RDN excluded.

### E. Explicit merchant learning

- [x] Category confirmation changes only that transaction by default and creates
  no enabled `auto_apply` alias.
- [x] A separate explicit “remember this merchant” action creates a
  household-scoped, audited, visible, reversible rule.
- [x] Web and Telegram expose the explicit remember choice where appropriate.

Tests: [x] one-off confirmation creates no rule; [x] explicit remember creates
rule; [x] household isolation; [x] disabled rule stops auto-application.

## P1 — Web product experience

### F. Budgeting dormant

- [x] Remove budget creation/utilization/fetching from active UI and navigation.
- [x] Keep existing backend/tables dormant; do not destructively migrate solely
  for alignment.
- [x] Docs describe budgeting as optional/future and a test verifies budget UI is
  absent.

### G. Responsive app shell

- [x] Desktop and mobile navigation are usable across `/`, `/transactions`,
  `/analytics`, `/reviews`, `/documents`, `/household`, and `/settings`.
- [x] Auth/session behavior is consistent and route rendering is tested.

### H. Overview

- [x] KPIs show monthly income, expense, net cashflow, and review count.
- [x] Prominent review action, category visualization, and linked recent
  transactions are present without budget utilization.
- [x] A responsive real chart renders 6–12 months of income, expense, and net with
  animation, tooltip, month labels, and IDR formatting.
- [x] Go/SQL owns all authoritative calculations.

### I. Transactions

- [x] Full ledger shows date, description/merchant, category, member/source,
  account, status, and amount.
- [x] Query-backed date/type/category/member/status/account/source/text filters work.
- [x] Detail exposes evidence, provenance, review state, and audit/corrections.

### J. Analytics

- [x] Dedicated page shows cashflow, monthly spending, category, merchant, member,
  and refund-adjusted analytics.
- [x] 3-, 6-, 12-month and custom ranges use deterministic APIs.

### K. Review Inbox

- [x] Dedicated page shows reason, amount, counterparty, date, source, candidates,
  and proposal/transaction state.
- [x] Valid actions include confirm, categorize, classify transfer, merge, reject,
  ignore, and open evidence against the shared review object.

### L. Documents

- [x] Page shows thumbnail, source, type, status, confidence, summary, links,
  review state, and received date.
- [x] Payslip detail retains deductions as metadata; receipt detail shows totals,
  items, and matched transaction.

### M. Settings

- [x] Expose Accounts, Categories, Merchants, Integrations, and System status.
- [x] Manage Jago/known accounts; safe category hierarchy/lifecycle; transparent,
  reversible merchant rules; Gmail/Telegram/sync/gateway state.

Required web tests: [x] routes render; [x] budget UI absent; [x] chart uses API;
[x] transaction filters; [x] shared review action; [x] household/Telegram state.

## P1 — Telegram finance assistant

### N. Finance-scoped intents

- [x] `ADD_EXPENSE`, `ADD_INCOME`, `CLARIFY_TRANSACTION`, and `HELP` foundation.
- [x] Add `CORRECT_TRANSACTION`, `SEARCH_TRANSACTIONS`, `GET_SPENDING`,
  `GET_CASHFLOW`, `GET_REVIEW_ITEMS`, and `UPLOAD_FINANCIAL_DOCUMENT`.
- [x] LLM interpretation yields validated parameters; Go/SQL selects records and
  computes totals; ledger disclosure is minimized.
- [x] Ambiguous correction/search asks a follow-up instead of guessing.

Tests: [x] spending; [x] cashflow; [x] search; [x] correction; [x] review list;
[x] ambiguity; [x] LLM cannot invent totals; [x] review binding remains intact.

### O. Inline Telegram actions

- [x] Category, transfer, and confirmation prompts use inline buttons.
- [x] Numeric sender/household authorization and exact callback binding are
  deterministic; natural-language replies remain supported.

## P2 — UX polish

- [x] Skeletons, useful empty states, toast feedback, and recoverable errors,
  including Ledger request recovery rather than an indefinite loading state.
- [x] Authenticated app route shells are not long-lived cached documents, so a
  deployment cannot leave the browser with stale Next.js chunks.
- [x] Subtle chart/page motion that never obscures financial values.
- [x] Responsive tables/cards and fully usable mobile layouts.
- [x] Accessible focus states and keyboard navigation.
- [x] Visual review confirms a calm, household-friendly, data-focused experience.

## Documentation and release gates

- [x] Product Alignment v2 and this V2 checklist are tracked.
- [x] README/current docs stay synchronized after each cohesive merge.
- [x] Member onboarding, image intake, transfer classification, merchant opt-in,
  and dormant budgeting are documented with relevant ADRs when implemented.
- [x] Schema changes are additive, migrated, and tested.
- [x] Each phase passes relevant Go tests and, when applicable, frontend build.
- [x] Final release passes full Go tests, frontend build, Compose validation,
  migrations, public smoke checks, and production state checks.
- [x] Each phase is committed, pushed, merged `--no-ff`, pushed to `main`, deployed
  only from updated `main`, and verified on the public URL.

## Execution order

1. [x] Household member APIs/domain model.
2. [x] Telegram link invite model and `/start` flow.
3. [x] Household UI (implemented and public route smoke verified).
4. [x] Telegram image intake and generic pipeline wiring.
5. [x] Jago outgoing-transfer semantics and known accounts.
6. [x] Explicit merchant remember flow; remove implicit learning.
7. [x] Remove budgeting from active UI.
8. [x] Build the routed app shell and all V2 pages.
9. [x] Expand Telegram intents and inline actions.
10. [x] Apply P2 polish and run final release verification.

Alignment v2 is complete only when every applicable criterion is `[x]` and the
intended flow is verified in production. Backend-only or UI-only fragments remain
partial until wired end to end.
