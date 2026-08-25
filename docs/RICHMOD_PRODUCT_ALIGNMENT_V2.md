# Richmod Product Alignment v2

**Document type:** Product alignment / implementation change request  
**Target:** Codex  
**Repository:** `raufimusaddiq/richmod`

> This document is an iteration snapshot, not a permanent source of truth. The latest explicit user instruction takes precedence. Update current repository documentation as implementation evolves.

## 1. Why this alignment exists

The current repository has a strong backend foundation, but the product experience and several important financial behaviors are not yet aligned with the intended use.

Main gaps:

- The web UI is effectively a single-page prototype.
- Budgeting is surfaced prominently even though the current product goal is tracking and analysis.
- Several backend capabilities exist but are not properly exposed in UI.
- Telegram text input/review exists, but Telegram image intake is not wired.
- Multi-member household onboarding is incomplete.
- A spouse cannot safely self-link a Telegram account.
- Jago outgoing transfer handling is not yet semantically correct.
- Merchant auto-learning is too aggressive.
- Telegram is still mostly a transaction parser instead of a finance-scoped assistant.

This iteration must fix those gaps without weakening the existing evidence-first, deterministic architecture.

## 2. Product direction

Richmod is currently a **household finance tracking and analysis system**.

Current product flow:

```text
TRACK
  ↓
UNDERSTAND
  ↓
CLASSIFY
  ↓
RECONCILE
  ↓
ANALYZE
  ↓
REVIEW
```

Budgeting is not part of the current active product experience. Do not force users to create budget limits or make budget utilization a primary dashboard concept.

Existing budget backend code may remain dormant to avoid destructive work, but budgeting must be removed from active navigation and primary UI for this iteration.

## 3. Architecture invariants

These remain mandatory:

```text
PostgreSQL = canonical financial state
Go = owner of financial state transitions
LLM output = untrusted structured input
LLM never mutates DB directly
Evidence is preserved
Ambiguity goes to Review Inbox
No hard-delete of canonical financial records
Jago incoming funding is not household income
Internal transfer is not household spending
Telegram replies bind deterministically to the exact review transaction
```

Core principle:

> LLM understands unstructured input. Go determines whether and how that understanding is allowed to change financial state.

## 4. Current implementation assessment

The repository already contains working or partially working support for:

```text
canonical ledger
transaction evidence
audit log
manual transaction input
Gmail integration
deterministic Bank Jago parser
Jago SPENDING_ONLY handling for incoming/pocket/RDN
Telegram natural-language income/expense input
Cloud LLM Gateway
transaction proposals
Review Inbox
Telegram review reply binding
generic document classifier
payslip extraction
receipt extraction
transaction screenshot extraction
analytics endpoints
LLM insight generation
```

Do not rewrite these components unless required for correctness or product exposure.

## 5. Priority order

```text
P0 Correctness and household onboarding
P1 Product UI redesign
P1 Telegram finance assistant expansion
P2 UX polish
```

Correctness comes before cosmetic polish.

---

# P0 — Correctness and household onboarding

## 6. Household member onboarding

### Problem

The data model already supports `household`, `user`, `household_member`, roles `OWNER | MEMBER`, and `telegram_identity`, but the app only has a one-time OWNER bootstrap.

There is no product flow to add another household member or safely link their Telegram account.

### Required behavior

Add a Household management area for OWNER users.

Recommended route:

```text
/household
```

OWNER can:

- view household members;
- add a member;
- deactivate a member if necessary;
- see Telegram connection status;
- generate/revoke a Telegram linking invitation.

Do **not** require the OWNER to manually discover or type another person's numeric Telegram ID.

### Telegram linking flow

1. OWNER creates a household member with `display_name`, `email`, role `MEMBER`.
2. System generates a short-lived, single-use Telegram invitation.
3. UI gives a link such as:

```text
https://t.me/<bot_username>?start=<single-use-token>
```

4. Spouse opens the bot and sends `/start <token>`.
5. Telegram provides the real `update.message.from.id`.
6. Backend validates invite and creates `telegram_identity` using the actual sender ID.
7. Invite becomes consumed.
8. Web UI shows `Telegram: Connected`.

Suggested table:

```text
telegram_link_invite
--------------------
id
household_id
user_id
token_hash
status
expires_at
created_by_user_id
created_at
consumed_at
```

Statuses:

```text
PENDING
CONSUMED
REVOKED
EXPIRED
```

Store only a hash of the secret token.

### Security requirements

- Telegram identity must be proven by a message from the actual Telegram account.
- Never trust Telegram username as identity.
- Do not use manually typed numeric Telegram ID as the primary proof.
- Invite is single-use, expiring, revocable, household-scoped, and audited.
- Only OWNER can create/revoke member invitations.

## 7. Telegram image intake

### Problem

Telegram text is wired, but direct image ingestion through the bot is not complete.

The intended product must support sending:

```text
payslip screenshot
receipt
bank screenshot
e-wallet screenshot
transfer proof
transaction history screenshot
other finance images
```

### Required flow

```text
Telegram image
    ↓
authenticate telegram_identity
    ↓
source_event = TELEGRAM_IMAGE
    ↓
download Telegram file
    ↓
validate size / MIME / dimensions
    ↓
attachment storage
    ↓
existing generic document pipeline
    ↓
Cloud LLM Gateway vision
    ↓
Go validation
    ↓
proposal / reconciliation / review
```

Do not create a receipt-only Telegram path. Reuse the existing generic document pipeline.

Image caption is supplemental context only. It may help extraction but cannot override document classification, amount, account identity, authorization, or deterministic rules.

Suggested bot responses:

```text
Slip gaji diterima. Sedang diproses.
Struk diterima. Aku akan coba cocokkan dengan transaksi yang sudah ada.
Aku tidak menemukan dokumen keuangan yang didukung dari gambar ini.
```

## 8. Fix Jago outgoing-transfer semantics

### Problem

The parser correctly recognizes `OUTGOING_TRANSFER` as requiring review, but the persistence path currently represents it as an `EXPENSE` with `NEEDS_REVIEW` before its meaning is known.

An outgoing transfer may actually be:

```text
household expense
own-account transfer
household-account transfer
investment/RDN movement
other non-spending movement
```

### Required behavior

Do not count an ambiguous outgoing transfer as expense before classification.

Preferred flow:

```text
Jago OUTGOING_TRANSFER
      ↓
transaction_proposal
      ↓
TRANSFER_CLASSIFICATION review
      ↓
user / deterministic known-account rule
      ├── EXPENSE
      ├── TRANSFER
      └── IGNORE / non-spending reference
```

If an interim canonical row is required for Review Inbox, it must be neutral and excluded from analytics while unresolved.

### Known-account support

Add or complete:

```text
known_account
-------------
id
household_id
user_id
institution
display_name
match_hint
relationship
active
```

Relationships:

```text
OWN_ACCOUNT
HOUSEHOLD_ACCOUNT
INVESTMENT_ACCOUNT
OTHER
```

Avoid storing unnecessary full account numbers when a masked stable matching hint is enough.

Decision policy:

```text
Jago -> OWN_ACCOUNT
    = TRANSFER
    = excluded from expense analytics

Jago -> HOUSEHOLD_ACCOUNT
    = TRANSFER by default

Jago -> INVESTMENT_ACCOUNT
    = non-spending in current MVP

Jago -> unknown external recipient
    = ask user
```

Telegram example:

```text
🟡 Transfer Bank Jago perlu diklasifikasikan

Rp1.000.000 → BCA ****1234

Transfer ini untuk apa?

[ Pengeluaran ]
[ Rekening sendiri ]
[ Rekening household ]
```

Natural-language replies must also work, e.g. `rekeningku sendiri`, `buat bayar tukang`, `transfer ke istri`.

LLM may interpret the reply, but Go determines the final transaction type.

## 9. Merchant learning must require explicit opt-in

### Problem

Current review resolution can create an `auto_apply` merchant alias after one category correction. This is too aggressive for multi-purpose merchants such as Shopee, Tokopedia, Grab, Gojek, supermarkets, and department stores.

### Required behavior

Resolving one transaction updates only that transaction.

It must **not** silently create a permanent auto-category rule.

The bot may later offer:

```text
PAMELLA DUA biasanya kamu kategorikan sebagai Groceries.
Gunakan kategori ini otomatis untuk transaksi berikutnya?

[ Ya ] [ Tidak ]
```

Only explicit confirmation may set:

```text
merchant_alias.auto_apply = true
created_from_user_confirmation = true
```

Policy:

```text
single transaction confirmation
    -> transaction only

explicit remember action
    -> permanent merchant rule

repeated same classifications
    -> system may suggest remembering
    -> never silently enable
```

---

# P1 — Product UI redesign

## 10. Remove budgeting from current product UI

For this iteration:

- remove Budget from active navigation;
- remove budget creation form from dashboard;
- remove monthly-limit emphasis;
- stop fetching budget data from the main dashboard;
- stop making budget utilization part of primary UX.

Do not destructively delete budget tables/handlers solely for this alignment. Keep them dormant or behind:

```text
FEATURE_BUDGETS=false
```

Documentation should describe budgeting as future/optional scope.

## 11. Required app shell

The application must no longer place every feature in one root page.

Desktop target:

```text
┌─────────────────────────────────────────────────────┐
│ Richmod                                             │
├───────────────┬─────────────────────────────────────┤
│ Overview      │                                     │
│ Transactions  │                                     │
│ Analytics     │             page content            │
│ Review Inbox  │                                     │
│ Documents     │                                     │
│ Household     │                                     │
│ Settings      │                                     │
└───────────────┴─────────────────────────────────────┘
```

Recommended routes:

```text
/
/transactions
/analytics
/reviews
/documents
/household
/settings
```

Mobile should use a drawer or bottom navigation and remain fully usable.

## 12. Overview page

Overview should answer:

```text
How much came in?
How much went out?
What is changing?
Where is money going?
What needs my attention?
```

### KPI cards

Show:

```text
Income this month
Expense this month
Net cashflow
Review count
```

Optional secondary metrics:

```text
expense change vs previous month
income change vs previous month
largest category
largest merchant
```

Do not show budget utilization.

### Cashflow chart

Replace pseudo bars/ranking presentation with a real responsive chart using a library such as Recharts.

Recommended:

```text
6-12 month line/area chart
Series:
Income
Expense
Net Cashflow
```

Requirements:

- animated initial render;
- tooltip;
- IDR formatting;
- month labels;
- responsive;
- no authoritative finance calculations in browser;
- data comes from deterministic backend analytics.

### Category visualization

Use donut/pie chart plus ranked values for category share.

### Recent transactions

Show a compact preview with:

```text
merchant/description
category
date
amount
source/status
```

Link to full Transactions page.

### Review alert

If review count > 0, show a prominent action such as:

```text
3 transaksi butuh bantuanmu
```

## 13. Transactions page

This becomes the primary ledger exploration page.

Row/column information:

```text
date
description / merchant
category
member/source
account
status
amount
```

Filters:

```text
date range
transaction type
category
member
status
account
source
search text
```

Prefer query-param-backed filters.

Transaction detail must expose provenance:

```text
Rp55.199
PAMELLA DUA
Household > Groceries

Evidence
✓ Bank Jago email
✓ Telegram clarification
✓ Receipt

Audit / corrections
...
```

A transaction must never appear as if it came from nowhere.

## 14. Analytics page

Move deep analytics away from Overview.

Required sections:

```text
cashflow trend
monthly spending trend
category distribution
merchant distribution
member contribution
refund-adjusted spending
```

Recommended controls:

```text
3 months
6 months
12 months
custom range
```

Backend may need range query parameters. Browser renders values; Go/SQL owns calculations.

## 15. Review Inbox page

Make Review Inbox first-class.

Display:

```text
reason
amount
merchant/counterparty
date
source
candidate matches
current transaction/proposal state
```

Actions:

```text
Confirm
Choose category
Classify transfer
Merge
Reject
Ignore
Open evidence
```

Web Review and Telegram Review must resolve the same underlying review object.

## 16. Documents page

Show more than image dimensions.

Display:

```text
thumbnail
source
document type
status
confidence
extraction summary
linked transaction(s)
review state
received date
```

Payslip detail may show period, employer, gross, allowances, deductions, and net pay. Deductions remain metadata only.

Receipt detail may show merchant, subtotal, tax, service, discount, total, items, and matched transaction.

## 17. Household page

Show:

```text
Household name
Members
Role
Telegram connection state
Invite/link Telegram
```

Example:

```text
Raufi        OWNER   Telegram connected
Wife         MEMBER  Telegram not connected
```

OWNER actions:

```text
Add member
Generate Telegram invite
Revoke invite
Deactivate member
```

## 18. Settings page

Expose relevant existing backend capabilities.

Sections:

```text
Accounts
Categories
Merchants
Integrations
System status
```

### Accounts

Manage Bank Jago `SPENDING_ONLY` and known owned/household accounts.

### Categories

Allow view/add/rename/activate/deactivate/hierarchy management without breaking historical references.

### Merchants

Show normalized merchant, aliases, default category, and auto-apply status. Merchant rules must be transparent and reversible.

### Integrations

Show Gmail, Telegram bot, last Gmail sync, and LLM gateway/system status when available. Expose existing Gmail connect flow here.

---

# P1 — Telegram finance assistant expansion

## 19. Required intents

Expand Telegram from transaction parser into a finance-scoped household assistant.

Required intents:

```text
ADD_EXPENSE
ADD_INCOME
CLARIFY_TRANSACTION
CORRECT_TRANSACTION
SEARCH_TRANSACTIONS
GET_SPENDING
GET_CASHFLOW
GET_REVIEW_ITEMS
UPLOAD_FINANCIAL_DOCUMENT
HELP
```

Budget intent is not required in current scope.

Examples:

```text
bulan ini paling banyak habis buat apa?
berapa pengeluaranku minggu ini?
cari transaksi pamella minggu lalu
yang pamella tadi salah, itu belanja rumah
```

LLM may interpret intent/time/search/correction meaning. Go/SQL must compute authoritative amounts and select records.

If transaction identity is ambiguous during correction, ask a follow-up instead of guessing.

## 20. Telegram inline actions

Add inline buttons where useful using Telegram `reply_markup`.

Examples:

```text
[ Makan ] [ Belanja ]
[ Transport ] [ Rumah ]
[ Lainnya ]
```

Transfer classification:

```text
[ Pengeluaran ]
[ Rekening sendiri ]
[ Household ]
```

Confirmation:

```text
[ Benar ] [ Ubah ]
```

Natural-language replies remain supported.

## 21. LLM boundary for Telegram queries

Pattern:

```text
Telegram
    ↓
LLM intent/parameter extraction
    ↓
Go validation
    ↓
deterministic query
    ↓
structured result
    ↓
optional response wording
```

Do not send the entire ledger to the LLM unnecessarily.

---

# P2 — UX polish

## 22. UX improvements

After correctness and information architecture are complete, add:

```text
skeleton loading
animated chart entry
subtle page transitions
toast feedback
responsive layouts
useful empty states
error recovery UI
mobile-friendly tables/cards
accessible focus states
keyboard navigation
```

Animation must be subtle and must never obscure financial accuracy.

Recommended dependencies if needed:

```text
Recharts
Motion / Framer Motion
```

## 23. Visual direction

Richmod should feel:

```text
modern
calm
clean
financial
household-friendly
data-rich without clutter
```

Avoid:

```text
generic admin-dashboard look
huge unused whitespace
everything boxed into identical cards
budgeting-app visual language
overly colorful fintech gamification
```

Charts and transaction data should be the visual focus.

---

# Backend/API alignment

## 24. Likely API additions

Expected new/changed endpoints may include:

```text
GET    /api/v1/household
GET    /api/v1/household/members
POST   /api/v1/household/members
PATCH  /api/v1/household/members/:id

POST   /api/v1/household/members/:id/telegram-invite
DELETE /api/v1/household/members/:id/telegram-invite

GET    /api/v1/transactions?...filters

GET    /api/v1/analytics/cashflow?range=...
GET    /api/v1/analytics/categories?range=...
GET    /api/v1/analytics/merchants?range=...
GET    /api/v1/analytics/members?range=...

GET    /api/v1/integrations/status
```

All endpoints must be household-scoped.

## 25. Likely database additions

Expected:

```text
telegram_link_invite
known_account
```

Review/proposal changes may be needed to represent unresolved outgoing transfers without incorrectly counting them as EXPENSE.

Prefer additive migrations. Do not destructively rewrite historical financial data.

## 26. Analytics correctness

Headline metrics may include only deterministic confirmed financial state.

```text
CONFIRMED INCOME
CONFIRMED EXPENSE
REFUND according to current deterministic policy
```

Unresolved transfer-classification rows must not count as expense.

Transfers remain excluded from household income/expense analytics.

Budget data must not influence the primary dashboard.

## 27. Multi-member attribution

After spouse Telegram onboarding:

```text
telegram_identity.user_id
    ↓
created_by_user_id
```

must attribute manual Telegram transactions to the correct member.

Automatic Bank Jago transactions may remain `Automatic / Household` unless member attribution is deterministically known. Do not guess which member made an automatic bank transaction.

---

# Documentation and testing

## 28. Documentation alignment

As part of implementation:

- update README current-phase language;
- update relevant docs under `docs/`;
- document member onboarding;
- document Telegram image intake;
- document transfer classification;
- document merchant-learning opt-in;
- move budgeting to optional/future scope.

Historical blueprint documents are not permanently authoritative. Repository docs should describe the current implemented product.

## 29. Required tests

### Household onboarding

```text
owner adds member
non-owner cannot add member
invite expires
invite is single use
invite cannot cross household
telegram sender ID binds correctly
reused token rejected
revoked token rejected
```

### Telegram image intake

```text
authorized image -> TELEGRAM_IMAGE source_event
unauthorized image -> no data exposure
payslip image -> document pipeline
receipt image -> document pipeline
invalid MIME -> rejected
oversized image -> rejected
duplicate Telegram update -> idempotent
```

### Jago transfer

```text
unknown outgoing transfer -> unresolved review
unresolved transfer excluded from spending
own account -> TRANSFER
household account -> TRANSFER
external expense -> EXPENSE
incoming Jago -> no income
RDN -> non-spending
```

### Merchant learning

```text
one category confirmation does not create auto_apply rule
explicit remember action creates rule
rule is household-scoped
rule can be disabled
```

### Web UI

```text
navigation routes render
budget UI absent
cashflow chart uses API data
transaction filters work
review action updates same backend review
household member state renders
Telegram connection state renders
```

### Telegram assistant

```text
GET_SPENDING
GET_CASHFLOW
SEARCH_TRANSACTIONS
CORRECT_TRANSACTION
review reply binding
inline callback authorization
ambiguous query asks follow-up
LLM cannot invent authoritative totals
```

---

# Acceptance criteria

## 30. Product

Alignment is complete when:

- App is no longer a single-page dashboard.
- Primary navigation exists.
- Budgeting is not shown as an active product feature.
- Overview focuses on tracking, analysis, and review.
- Cashflow uses a real line/area chart.
- Deep analytics have their own page.
- Transactions have a full ledger page.
- Review Inbox has its own page.
- Documents have their own page.
- Household/member management exists.
- Settings expose relevant existing backend capabilities.

## 31. Household

- OWNER can add spouse/member.
- Spouse can self-link Telegram using a secure single-use invite.
- Manual Telegram ID discovery is not required.
- Member transaction attribution works.

## 32. Telegram

- Text transaction input still works.
- Existing review reply binding still works.
- Telegram image intake works.
- Payslip can be sent directly to bot.
- Receipt can be sent directly to bot.
- Finance query intents work.
- Inline review buttons work where appropriate.

## 33. Jago

- Incoming funding remains non-income.
- Pocket/RDN movement remains non-spending.
- Merchant payments remain expense candidates.
- Outgoing transfer is not prematurely counted as expense.
- Own-account and household transfers can resolve to TRANSFER.
- Ambiguous external transfer asks the user.

## 34. Merchant learning

- One transaction correction does not silently create a permanent rule.
- Permanent auto-category requires explicit confirmation.

## 35. Architecture

- LLM never directly mutates canonical financial state.
- Deterministic Go validation remains mandatory.
- Evidence is retained.
- Audit trail remains intact.
- Cross-household authorization remains enforced.

---

# Codex execution guidance

## 36. Suggested implementation order

```text
1. Follow AGENTS.md worktree/branch workflow.
2. Add household member APIs/domain model.
3. Add Telegram link invite model and flow.
4. Add Household UI.
5. Add Telegram image webhook/intake.
6. Connect Telegram images to existing document pipeline.
7. Fix Jago outgoing-transfer representation and review semantics.
8. Add known-account support.
9. Remove implicit merchant auto-learning.
10. Add explicit merchant remember flow.
11. Remove budgeting from current UI.
12. Refactor frontend into app shell + routes.
13. Build Overview with real charts.
14. Build Transactions page.
15. Build Analytics page.
16. Build Review Inbox page.
17. Build Documents page.
18. Build Settings page.
19. Expand Telegram finance query/correction intents.
20. Add inline Telegram review actions.
21. Update living docs.
22. Run full tests/builds.
23. Merge only after verification according to AGENTS.md.
```

Do not implement everything as one giant unreviewable change if smaller cohesive changes are safer.

## 37. Non-goals for this iteration

Do not add:

```text
stock portfolio tracking
crypto tracking
gold tracking
broker integration
market data
investment P&L
net worth
bank transfer execution
payment execution
general-purpose Telegram agent
budgeting-first workflow
```

Do not redesign the financial core unless correctness requires it.

## 38. Codex completion report

For each repository-changing request, report:

```text
branch
worktree
commit(s)
tests run
frontend build result
Go test result
migration verification
merge commit
main push
deployment result if requested
known limitations
```

Do not claim a feature is complete if it only exists in backend code but is not wired into the intended product flow.

## 39. Final product target

After this alignment, Richmod should feel like:

> A household finance tracker that quietly captures transactions, understands financial documents, asks only when context is missing, and gives a clear visual explanation of where household money is going.

It should **not** feel like a budget planner that forces manual spending limits.

It should **not** feel like a backend demo where every capability is compressed into one page.
