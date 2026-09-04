# Family Finance OS — System Analysis, Product Specification, and Codex Development Guide

**Status:** Historical MVP blueprint. Current product direction is governed by
[`RICHMOD_PRODUCT_ALIGNMENT_V2.md`](RICHMOD_PRODUCT_ALIGNMENT_V2.md), and delivery
status is tracked in [`MVP_COMPLETION_CHECKLIST.md`](MVP_COMPLETION_CHECKLIST.md).
**Primary implementation target:** Codex  
**Backend:** Go  
**Frontend:** JavaScript / Next.js / React  
**Database:** PostgreSQL  
**LLM access:** Cloud LLM Gateway only  
**Primary chat channel:** Telegram  
**Primary automated spending source:** Bank Jago transaction email  
**MVP scope:** Household income + expense tracking only  
**Explicitly deferred:** Assets, stocks, mutual funds, gold, crypto, portfolio valuation, broker integration

> Product Alignment v2 supersedes conflicting product-experience requirements in
> this blueprint. Budgeting is dormant/optional rather than part of the active UI,
> outgoing Jago transfers remain neutral until classified, and merchant
> auto-learning requires a separate explicit opt-in. The deterministic
> architecture and evidence/audit requirements below remain binding.

---

# 1. Purpose

Build a self-hosted household finance system that automatically records and enriches income and spending while keeping financial state deterministic and auditable.

The system must combine:

- deterministic transaction parsing;
- Bank Jago spending automation;
- manual transaction entry;
- Telegram-based interaction;
- image/document understanding through LLM vision;
- payslip extraction for income;
- receipt and screenshot understanding;
- human-in-the-loop clarification for ambiguous transactions;
- reconciliation and duplicate prevention;
- spending categorization;
- dashboard analytics;
- budgets;
- LLM-generated financial insights based on precomputed metrics.

The system is not a general-purpose AI assistant. It is a narrowly scoped household finance application.

---

# 2. Core Product Principle

The most important rule in this system:

> **LLM understands unstructured input. Go determines whether and how that understanding is allowed to change financial state.**

The LLM is never the source of truth.

The source of truth is:

1. PostgreSQL;
2. deterministic Go business rules;
3. explicit user confirmations;
4. retained evidence and audit history.

LLM responsibilities:

- classify unstructured documents;
- extract structured information;
- interpret natural-language Telegram messages;
- suggest categories;
- normalize merchant names;
- generate narrative insights from precomputed analytics.

LLM must never:

- directly mutate the database;
- execute SQL;
- call arbitrary HTTP endpoints;
- access filesystem or shell;
- execute bank transfers;
- access banking credentials;
- execute broker orders;
- override deterministic financial rules;
- silently create financial records when validation fails.

General failure principle:

```text
uncertain -> REVIEW
never -> GUESS
```

---

# 3. Final MVP Scope

## 3.1 Included

MVP includes:

- household and users;
- login/authentication;
- OWNER / MEMBER roles;
- bank/cash/e-wallet accounts used for transaction context;
- categories;
- merchants and merchant aliases;
- canonical transaction ledger;
- income;
- expense;
- Bank Jago email ingestion;
- Jago-specific `SPENDING_ONLY` policy;
- Telegram bot;
- natural-language Telegram transaction input;
- generic image/document intake;
- payslip image extraction;
- receipt image extraction;
- bank/e-wallet screenshot extraction;
- transaction screenshot extraction;
- invoice/document classification;
- transaction proposals;
- reconciliation / deduplication;
- Review Inbox;
- Telegram interactive review;
- reply-to-transaction conversation binding;
- category learning through confirmed deterministic rules;
- dashboard;
- spending analytics;
- budget support;
- LLM insights;
- audit log;
- backups;
- observability;
- production Docker deployment.

## 3.2 Deferred

Do NOT implement during MVP:

- canonical transfer creation and transfer-ledger workflows;
- refund creation workflows;
- adjustment creation workflows;
- stock portfolio;
- broker API;
- broker statement processing;
- mutual fund tracking;
- gold tracking;
- cryptocurrency tracking;
- asset market-price feeds;
- investment tax lots;
- realized/unrealized portfolio P&L;
- portfolio allocation;
- investment transactions;
- net-worth portfolio valuation.

Future asset work must be implemented as a separate module after the transaction system is stable.

Codex must not pre-build unused asset tables or asset APIs during MVP.

---

# 4. User Mental Model

The application behaves like a **household financial inbox**.

Users can:

- spend using Bank Jago and have the expense appear automatically;
- send a text message to Telegram;
- send a receipt photo;
- send a payslip screenshot;
- send a bank/e-wallet screenshot;
- send other finance-related images;
- answer clarification questions from the Telegram bot.

The system turns those inputs into canonical financial records only after deterministic validation.

Example:

```text
Bank Jago email
    |
    v
Rp55,199 -> PAMELLA DUA
    |
    v
merchant purpose unknown
    |
    v
Telegram:
"Rp55.199 di PAMELLA DUA tadi buat apa?"
    |
User reply:
"belanja bulanan, sabun dan kebutuhan dapur"
    |
    v
LLM structured interpretation
    |
    v
Go validation
    |
    v
Expense:
Household > Groceries
note = "belanja bulanan, sabun dan kebutuhan dapur"
```

---

# 5. High-Level Architecture

```text
                       +-------------------+
                       |      Telegram     |
                       +---------+---------+
                                 |
                                 v
                       +-------------------+
                       |  Telegram Webhook |
                       +---------+---------+
                                 |
                                 v
+-------------+       +---------------------+       +------------------+
| Gmail / Jago| ----> |      Finance API    | <---- |   Finance Web    |
+-------------+       +----------+----------+       +------------------+
                                 |
                                 v
                       +---------------------+
                       |     PostgreSQL      |
                       +----------+----------+
                                  ^
                                  |
                       +----------+----------+
                       |    Finance Worker   |
                       +----------+----------+
                                  |
                        +---------+---------+
                        | Cloud LLM Gateway |
                        +---------+---------+
                                  |
                         +--------+--------+
                         | Provider A/B/C  |
                         +-----------------+
```

Deployable containers:

```text
finance-web
finance-api
finance-worker
finance-db
```

No local LLM container.

---

# 6. Recommended Repository Structure

```text
family-finance/
├── apps/
│   ├── api/
│   │   ├── cmd/
│   │   │   └── api/
│   │   ├── internal/
│   │   │   ├── auth/
│   │   │   ├── account/
│   │   │   ├── transaction/
│   │   │   ├── category/
│   │   │   ├── merchant/
│   │   │   ├── source/
│   │   │   ├── reconciliation/
│   │   │   ├── telegram/
│   │   │   ├── gmail/
│   │   │   ├── jago/
│   │   │   ├── document/
│   │   │   ├── llm/
│   │   │   ├── review/
│   │   │   ├── budget/
│   │   │   ├── analytics/
│   │   │   ├── insight/
│   │   │   ├── audit/
│   │   │   └── jobs/
│   │   └── go.mod
│   │
│   ├── worker/
│   │   ├── cmd/
│   │   │   └── worker/
│   │   └── go.mod
│   │
│   └── web/
│       ├── app/
│       ├── components/
│       ├── lib/
│       ├── public/
│       └── package.json
│
├── db/
│   ├── migrations/
│   ├── queries/
│   └── seeds/
│
├── prompts/
│   ├── finance_intent_v1.md
│   ├── telegram_transaction_extract_v1.md
│   ├── document_classify_v1.md
│   ├── payslip_extract_v1.md
│   ├── receipt_extract_v1.md
│   ├── transaction_screenshot_extract_v1.md
│   ├── category_classify_v1.md
│   ├── merchant_normalize_v1.md
│   └── finance_insight_v1.md
│
├── infra/
│   ├── docker/
│   ├── compose/
│   └── reverse-proxy/
│
├── docs/
│   ├── adr/
│   ├── runbooks/
│   └── architecture/
│
├── scripts/
└── README.md
```

---

# 7. Technology Decisions

## 7.1 Backend

Use Go only.

Recommended:

- Go 1.24+;
- `net/http`;
- `chi` router;
- `pgx/v5`;
- `sqlc`;
- `goose` migrations;
- `slog`;
- `testify` where useful;
- `http.Client` with explicit timeout;
- standard-library-first approach.

Do not introduce a large framework unless an ADR justifies it.

## 7.2 Frontend

Use:

- Next.js;
- React;
- JavaScript;
- Tailwind CSS;
- TanStack Query;
- Recharts;
- shadcn-compatible UI patterns if useful.

Do not use TypeScript application code for this project unless the user explicitly changes this requirement.

## 7.3 Database

PostgreSQL.

Money must use:

```text
NUMERIC / DECIMAL
```

Never use floating point for financial amounts.

MVP ledger currency is IDR only. Store amounts as whole-rupiah `NUMERIC(20,0)`
and reject every non-`IDR` canonical transaction at the database boundary.
Multi-currency support is deferred and requires a future ADR and migration.

Time must use:

```text
TIMESTAMPTZ
```

Household display timezone:

```text
Asia/Jakarta
```

All finance application and database sessions operate in `Asia/Jakarta` (GMT+7).
Store instants as `TIMESTAMPTZ`; resolve relative dates, calendar periods, and
user-facing timestamps in that timezone.

## 7.4 Background Jobs

Use PostgreSQL-backed jobs first.

Pattern:

```sql
SELECT ...
FOR UPDATE SKIP LOCKED
```

No Redis, Kafka, RabbitMQ, or other queue for MVP unless an ADR proves the need.

---

# 8. Canonical Financial Model

## 8.1 Transaction Types

```text
INCOME
EXPENSE
TRANSFER
REFUND
ADJUSTMENT
```

## 8.2 Transaction Status

```text
PENDING
CONFIRMED
NEEDS_REVIEW
VOIDED
```

Never hard-delete financial transactions.

Corrections must be auditable.

The HTTP API enforces explicit status transitions: `PENDING` or `NEEDS_REVIEW`
may become `CONFIRMED`, while any non-voided transaction may become `VOIDED`.
Every transition is household-scoped and audited; no endpoint hard-deletes it.

## 8.3 Core Transaction Fields

Suggested fields:

```text
id
household_id
account_id
type
status
amount
currency
transaction_at
merchant_id
category_id
description
note
created_by
created_at
updated_at
confirmed_at
```

Optional useful fields:

```text
counterparty_name
external_reference
source_confidence
classification_confidence
```

---

# 9. Evidence-First Model

Every external or manual input is evidence.

A canonical transaction may have multiple evidence records.

Example:

```text
Expense Rp185,000
      |
      +-- Bank Jago email
      |
      +-- Telegram reply
      |
      +-- receipt photo
```

Never delete source evidence because a duplicate was found.

## 9.1 `source_event`

Every inbound event first becomes a `source_event`.

Suggested:

```text
id
household_id
source_type
external_id
received_at
raw_payload_ref
payload_hash
processing_status
parser_name
parser_version
created_at
```

Source types:

```text
BANK_EMAIL
TELEGRAM_TEXT
TELEGRAM_IMAGE
WEB_MANUAL
WEB_IMAGE
SYSTEM
```

Possible future values may be added, but do not create investment-specific types now.

## 9.2 `transaction_evidence`

Suggested:

```text
id
transaction_id
source_event_id
evidence_type
confidence
metadata_json
created_at
```

A transaction can have multiple evidence records.

---

# 10. Account Model

Accounts provide transaction context.

Suggested account types:

```text
BANK
CASH
EWALLET
OTHER
```

MVP does not require full balance reconciliation for every account.

Important account policy field:

```text
tracking_policy
```

Initial policies:

```text
FULL_LEDGER
SPENDING_ONLY
REFERENCE_ONLY
```

Bank Jago transaction account must use:

```text
SPENDING_ONLY
```

---

# 11. Bank Jago — Final MVP Policy

Bank Jago is treated as the household's primary **transaction / spending account**.

The user does not want incoming transfers to Jago treated as household income because Jago is funded from already-planned money originating elsewhere.

Therefore:

> **Jago outgoing spending matters. Jago incoming funding does not become INCOME.**

## 11.1 Observed Jago Email Families

The provided Gmail samples show email families such as:

```text
Kamu telah membayar ke <MERCHANT>💸
Kamu melakukan transaksi menggunakan kartu debit Jago
Kamu telah melakukan transfer💸
Asik, kamu telah menerima sejumlah uang💰
Kamu telah menerima uang di Kantong ...
Kamu memindahkan uang dari salah satu Kantong kamu
Kamu telah memindahkan uang ke Kantong lain
Pemindahan dana antar Kantong
Penarikan dana dari Kantong RDN berhasil
Transaksi di Kantong Bersama
```

These subject families must be modeled explicitly rather than relying only on LLM classification.

## 11.2 Known Jago Parsing Strategy

For known Jago templates:

```text
Gmail
  |
  v
sender/authentication validation
  |
  v
Jago subject-family detection
  |
  v
deterministic HTML parser
  |
  v
structured Jago event
  |
  v
SPENDING_ONLY policy
  |
  v
transaction proposal / ignore / review
```

Do not call the LLM for normal known templates unless deterministic extraction fails.

## 11.3 Fields to Extract

Depending on template:

```text
from_account
to_name
to_account_or_hint
amount
transaction_at
status
merchant
acquirer
merchant_location
terminal_id
reference
transaction_channel
```

Store only what is necessary.

Sensitive identifiers should be masked in logs and UI unless truly needed.

## 11.4 Jago Decision Table

### Merchant / QR payment

Examples:

```text
Kamu telah membayar ke ...
```

Behavior:

```text
direction = OUTGOING
financial_effect = EXPENSE_CANDIDATE
```

If merchant/category is confidently known:

```text
CONFIRMED EXPENSE
```

If purpose/category is ambiguous:

```text
NEEDS_REVIEW
Telegram clarification
```

### Debit card transaction

Example:

```text
Kamu melakukan transaksi menggunakan kartu debit Jago
```

Behavior:

```text
OUTGOING -> EXPENSE_CANDIDATE
```

Use merchant alias/category rules first.

### Outgoing transfer

Example:

```text
Kamu telah melakukan transfer💸
```

This is not automatically an expense.

Deterministic resolution:

```text
recipient == known own account
    -> TRANSFER / not spending

recipient == known household account
    -> TRANSFER / not spending by default

recipient == investment/RDN account
    -> ignore from MVP spending / TRANSFER context

recipient unknown/external
    -> EXPENSE_CANDIDATE
    -> normally ask purpose through Telegram
```

### Incoming money

Examples:

```text
Asik, kamu telah menerima sejumlah uang💰
Kamu telah menerima uang di Kantong ...
```

Behavior:

```text
source_event retained
NO canonical INCOME
NO spending impact
```

May be stored as ignored/reference evidence for debugging and future reconciliation.

### Jago pocket movement

Examples:

```text
Kamu memindahkan uang ...
Pemindahan dana antar Kantong
```

Behavior:

```text
INTERNAL TRANSFER
NO household expense
NO household income
```

### RDN-related movement

Behavior in MVP:

```text
NOT SPENDING
NOT INCOME
NO asset modeling
```

Keep as source evidence only if received.

---

# 12. Known Accounts and Recipients

To prevent false expenses, create:

## 12.1 `known_account`

Fields:

```text
id
household_id
name
owner_user_id
institution
account_type
match_token
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

Do not store unnecessary full bank account numbers if a stable masked/match token is sufficient.

## 12.2 `known_recipient`

Fields:

```text
id
household_id
normalized_name
relationship
default_treatment
notes
created_at
```

Default treatments may include:

```text
TRANSFER
EXPENSE
ASK
```

Never automatically mark a new external recipient as safe based only on LLM output.

---

# 13. Income Model

MVP income is intentionally simpler than expense automation.

Primary supported sources:

```text
1. Payslip image through Telegram/dashboard
2. Manual income input
3. Future optional salary-bank ingestion
```

Bank Jago incoming transfers must not create income.

---

# 14. Generic Image / Document Intake

All finance-related images use one generic pipeline.

Do not build a receipt-only upload flow.

Supported document classes:

```text
RECEIPT
PAYSLIP
BANK_TRANSACTION_SCREENSHOT
TRANSFER_PROOF
EWALLET_SCREENSHOT
BILL_OR_INVOICE
TRANSACTION_HISTORY_SCREENSHOT
OTHER_FINANCIAL_DOCUMENT
NON_FINANCIAL_OR_UNSUPPORTED
```

## 14.1 Image Pipeline

```text
Telegram / Web image
       |
       v
source_event
       |
       v
attachment validation
       |
       v
hash + duplicate check
       |
       v
secure storage
       |
       v
Cloud LLM Gateway
document.classify
       |
       v
document-specific structured extraction
       |
       v
Go schema validation
       |
       v
transaction_proposal(s)
       |
       v
reconciliation
       |
       +--> canonical transaction
       |
       +--> review inbox
```

## 14.2 Upload Validation

Required:

- size limit;
- MIME validation;
- supported extension validation;
- SHA-256;
- duplicate attachment detection;
- image dimension guard;
- EXIF stripping where appropriate;
- secure storage path;
- no executable processing;
- no trusting user-provided filename.

---

# 15. Payslip Processing

Payslip images may arrive from Telegram or dashboard.

The LLM vision extractor should return a structured payload such as:

```json
{
  "document_type": "PAYSLIP",
  "period": "2026-08",
  "employer": "Example Corp",
  "gross_pay": "18500000",
  "allowances": [
    {
      "name": "Allowance",
      "amount": "1000000"
    }
  ],
  "deductions": [
    {
      "name": "Tax",
      "amount": "1500000"
    }
  ],
  "net_pay": "16000000",
  "currency": "IDR",
  "confidence": 0.97
}
```

## 15.1 Deterministic Payslip Rules

Required validation:

```text
currency valid
amounts parse as decimal
net_pay > 0
period parseable
gross >= net where applicable
arithmetic sanity check where enough fields exist
```

LLM does not directly create the income transaction.

Instead:

```text
PayslipExtraction
     |
     v
IncomeProposal
     |
     v
deterministic validation
     |
     v
reconciliation
     |
     v
canonical INCOME or REVIEW
```

## 15.2 Payroll Deductions

Do not automatically convert payslip deductions into household expenses.

Examples:

- tax;
- BPJS;
- payroll deductions;
- internal employer deductions.

These would distort normal household spending analytics.

Store the breakdown as payslip metadata.

## 15.3 Duplicate Income Prevention

If salary-bank ingestion is enabled in the future:

```text
payslip net pay
+
salary bank credit
=
ONE income transaction with multiple evidence records
```

For MVP, salary-bank ingestion can remain disabled.

---

# 16. Receipt Processing

Receipt extraction should return:

```text
merchant
date/time
currency
subtotal
tax
service_charge
discount
total
items[]
payment_method_hint
```

Validate:

```text
total >= 0
subtotal/tax/service arithmetic where available
currency supported
date plausible
```

A receipt may:

- enrich an existing Jago transaction;
- create a manual expense proposal;
- remain in Review Inbox if no match exists.

Never create a second expense merely because a receipt was uploaded when a bank transaction already exists.

---

# 17. Transaction Screenshot Processing

For screenshots of transaction history, one image may contain multiple transactions.

LLM output:

```json
{
  "document_type": "TRANSACTION_HISTORY_SCREENSHOT",
  "transactions": [
    {
      "amount": "75000",
      "direction": "OUT",
      "merchant": "Example",
      "transaction_at": "..."
    }
  ]
}
```

Each extracted row becomes its own proposal and is reconciled independently.

Do not treat the screenshot itself as one transaction.

---

# 18. Invoice / Bill Safety

An invoice is not proof of payment.

Therefore:

```text
Invoice detected
    |
    v
document retained
    |
    v
optional payable proposal
    |
    v
NO canonical EXPENSE
until payment evidence or explicit confirmation
```

This rule is deterministic and cannot be overridden by LLM confidence.

---

# 19. Telegram as Primary Financial Assistant Interface

Telegram has two roles:

1. manual finance input;
2. interactive Review Inbox / clarification.

The bot is not a general-purpose chatbot.

## 19.1 Telegram Authentication

Authorize by immutable Telegram numeric user ID.

Do not authorize by username.

Validate:

```text
X-Telegram-Bot-Api-Secret-Token
```

Maintain:

```text
telegram_user_id -> household_user_id
```

Unauthorized users receive no financial information.

## 19.2 Supported Intents

Allowed:

```text
ADD_EXPENSE
ADD_INCOME
ADD_REFUND
CLARIFY_TRANSACTION
CORRECT_TRANSACTION
ATTACH_DOCUMENT
QUERY_SPENDING
QUERY_CASHFLOW
QUERY_BUDGET
QUERY_TRANSACTION
REQUEST_SUMMARY
CONFIRM_REVIEW
REJECT_REVIEW
HELP
```

Denied:

```text
SHELL
FILESYSTEM
GENERAL_AGENT
SECRETS
ARBITRARY_HTTP
BANK_TRANSFER
WITHDRAWAL
BROKER_ORDER
CREDENTIAL_STORAGE
```

## 19.3 Commands

Useful commands:

```text
/start
/help
/add
/income
/receipt
/spending
/cashflow
/budget
/review
```

Natural language remains supported.

---

# 20. Telegram Manual Transaction Input

Example:

```text
"makan hokben 85rb tadi siang"
```

Flow:

```text
Telegram message
       |
       v
Finance Intent Gate
       |
       v
Cloud LLM Gateway
telegram.transaction.extract
       |
       v
strict structured JSON
       |
       v
Go validation
       |
       v
TransactionProposal
       |
       v
reconciliation
       |
       v
transaction / review
```

Example extraction:

```json
{
  "type": "EXPENSE",
  "amount": "85000",
  "currency": "IDR",
  "merchant": "HokBen",
  "relative_time": "today afternoon",
  "category_candidate": "Food",
  "note": null
}
```

Relative dates must be resolved in Go using household timezone/context.

---

# 21. Human-in-the-Loop Telegram Review

This is a core MVP feature, not an optional enhancement.

The system should avoid repeatedly asking users about obvious transactions, but should actively ask when context is missing.

## 21.1 When to Ask

Suggested deterministic policy:

```text
known merchant + confirmed default category
    -> auto categorize

strong deterministic category rule
    -> auto categorize

high-confidence LLM category + non-sensitive normal merchant
    -> may auto categorize based on configured threshold

unknown merchant
    -> ask

generic bank transfer
    -> ask

unknown purpose
    -> ask

multiple plausible categories
    -> ask

conflicting evidence
    -> ask

duplicate ambiguity
    -> ask
```

False automatic classification is worse than a clarification question.

## 21.2 Review Push Example

Bot message:

```text
🟡 Butuh sedikit bantuan

Rp55.199 → PAMELLA DUA
8 Agu 2026, 15:29

Ini pengeluaran untuk apa?
```

Optional inline buttons:

```text
[ Makan ] [ Belanja ]
[ Transport ] [ Rumah ]
[ Lainnya ]
```

The user can also reply in natural language.

## 21.3 Reply Binding

Never ask the LLM to guess which transaction a reply refers to when Telegram reply metadata can identify it.

Create:

### `review_request`

```text
id
household_id
transaction_id
review_type
telegram_chat_id
telegram_message_id
status
created_at
expires_at
resolved_at
```

Status:

```text
OPEN
RESOLVED
EXPIRED
CANCELLED
```

On user reply:

```text
reply_to_message_id
       |
       v
review_request.telegram_message_id
       |
       v
exact transaction_id
```

This allows multiple review questions to exist at the same time safely.

## 21.4 Natural-Language Clarification

Example reply:

```text
"belanja bulanan sama istri, beli sabun sama kebutuhan dapur"
```

LLM returns:

```json
{
  "intent": "CLARIFY_TRANSACTION",
  "category_candidate": "Household > Groceries",
  "purpose": "Belanja bulanan",
  "note": "Sabun dan kebutuhan dapur"
}
```

Then Go:

```text
validate review request belongs to user/household
validate transaction still editable
validate category exists
validate reply schema
audit mutation
apply changes
mark review resolved
```

## 21.5 Multi-Turn Review

The bot may ask for confirmation:

```text
Aku catat sebagai Travel > Accommodation,
catatan "patungan hotel". Benar?

[ Benar ] [ Ubah ]
```

Conversation state must be explicit.

Suggested table:

### `review_conversation`

```text
id
review_request_id
state
context_json
last_message_at
created_at
updated_at
```

Suggested states:

```text
AWAITING_PURPOSE
AWAITING_CATEGORY
AWAITING_CONFIRMATION
RESOLVED
```

Do not keep financial conversation state only in LLM context.

Current implementation persists `review_request` and `review_conversation`.
Outbound Telegram message IDs are written back after `sendMessage` succeeds, and
incoming replies bind by the exact private chat ID plus `reply_to_message_id`.
Deterministic category-name matching runs before the cloud gateway; Go validates
all gateway suggestions against active household categories. Web confirmation,
web rejection, and reconciliation merges resolve or cancel the same Telegram
review record atomically.

---

# 22. Merchant and Category Learning

The system may learn only through explicit confirmed deterministic records.

Example:

```text
PAMELLA DUA -> Household > Groceries
```

After repeated confirmed classification, bot may ask:

```text
PAMELLA DUA biasanya kamu kategorikan sebagai Groceries.
Pakai otomatis ke depannya?

[ Ya ] [ Tidak ]
```

If confirmed, create/update:

### `merchant_alias`

```text
id
household_id
raw_name
normalized_merchant_id
default_category_id
auto_apply
created_from_user_confirmation
created_at
```

LLM itself must not silently create permanent merchant rules.

---

# 23. Category Model

Support hierarchical categories.

Example:

```text
Food
├── Dining
├── Coffee
└── Delivery

Household
├── Groceries
├── Utilities
├── Maintenance
└── Supplies

Transport
├── Fuel
├── Ride Hailing
└── Parking

Entertainment
├── Subscription
└── Leisure
```

Suggested table:

```text
category
--------
id
household_id
parent_id
name
slug
active
sort_order
created_at
```

Categories may be seeded and customizable.

The initial household seed uses Indonesian expense categories, including food and
drink, household, transport, health, family, education, personal spending,
entertainment, subscriptions, travel, bills, donations, tax/fees, and other.
The seed is idempotent and never overwrites user-customized categories.

---

# 24. Transaction Proposal Layer

All untrusted extraction paths should produce a proposal before canonical mutation.

### `transaction_proposal`

Suggested fields:

```text
id
household_id
source_event_id
proposed_type
amount
currency
transaction_at
merchant_raw
counterparty_raw
category_candidate_id
description
note
confidence
proposal_status
metadata_json
created_at
```

Statuses:

```text
PENDING
ACCEPTED
REJECTED
NEEDS_REVIEW
MERGED
```

Sources that should use proposals:

- Telegram natural language;
- image extraction;
- unknown email template fallback;
- ambiguous Jago transfer;
- imported manual evidence.

Known deterministic Jago merchant payment may be allowed to move directly through validation into canonical transaction, but preserving a common proposal stage is preferred for audit consistency.

---

# 25. Reconciliation and Duplicate Prevention

Core principle:

> **Deduplication links evidence. It never deletes evidence.**

## 25.1 Candidate Generation

Use deterministic candidate generation first.

Initial matching window:

```text
±72 hours
```

Candidate filters:

```text
same household
same currency
compatible transaction type
similar amount
time proximity
merchant/counterparty similarity
source relationship
```

## 25.2 Suggested Score

```text
amount match        0.45
time proximity      0.20
merchant similarity 0.20
account/source hint 0.10
category similarity 0.05
```

## 25.3 Thresholds

Initial defaults:

```text
>= 0.92
    auto-link only if exactly one strong unambiguous candidate

0.70 - 0.92
    NEEDS_REVIEW

< 0.70
    keep separate
```

Additional hard requirements for auto-link:

```text
same currency
strong amount match
no competing candidate
no conflicting direction
```

False merge is worse than missed merge.

All merges must be reversible.

---

# 26. Example Reconciliation Scenarios

## 26.1 Jago + Telegram

Jago:

```text
Rp185,000
Merchant: Sushi Hiro
```

Telegram:

```text
"dinner sushi tadi 185rb"
```

Result:

```text
ONE canonical expense
Evidence:
- Jago email
- Telegram text
```

## 26.2 Jago + Receipt

Jago:

```text
Rp55,199
PAMELLA DUA
```

Receipt:

```text
Rp55,199
PAMELLA DUA
```

Result:

```text
ONE expense
Evidence:
- Jago email
- receipt image
```

## 26.3 Payslip + Future Bank Credit

Payslip:

```text
net pay = Rp16,000,000
```

Future salary-bank source:

```text
credit = Rp16,000,000
```

Result:

```text
ONE INCOME
Evidence:
- payslip
- bank credit
```

---

# 27. Review Inbox

Review Inbox is a first-class product feature.

Types:

```text
UNKNOWN_MERCHANT
UNKNOWN_PURPOSE
AMBIGUOUS_CATEGORY
POSSIBLE_DUPLICATE
CONFLICTING_EVIDENCE
UNKNOWN_EMAIL_TEMPLATE
RECEIPT_MISMATCH
DOCUMENT_EXTRACTION_LOW_CONFIDENCE
TRANSFER_CLASSIFICATION
MANUAL_CORRECTION
```

Actions:

```text
Confirm
Edit
Merge
Different
Ignore
Open evidence
Ask via Telegram
```

The web dashboard and Telegram review must point to the same underlying review item.

Current implementation uses the `NEEDS_REVIEW` canonical transaction as the web
review identifier and keeps its proposal and source evidence linked. Confirm and
reject update all three records atomically. Reconciliation candidates use the
documented deterministic score and merge only same-household, same-direction,
same-currency, exact-amount records within 72 hours. Merge records are audited
and reversible; original transactions and evidence are retained.

---

# 28. Cloud LLM Gateway

No Ollama and no direct provider SDK use inside finance domain logic.

Architecture:

```text
Finance API / Worker
        |
        v
 Cloud LLM Gateway
        |
   +----+----+----+
   |         |    |
Provider A   B    C
```

Finance application secrets should ideally contain only:

```text
LLM_GATEWAY_BASE_URL
LLM_GATEWAY_API_KEY
```

Provider credentials belong inside gateway infrastructure.

## 28.1 Logical Model Policies

Use logical routes:

```text
finance.intent.small
finance.telegram.extract
finance.transaction.extract
finance.document.classify
finance.receipt.vision
finance.payslip.vision
finance.transaction_screenshot.vision
finance.classification
finance.insight
```

Business code must not hardcode vendor/model names.

## 28.2 Gateway Responsibilities

- authentication;
- logical route selection;
- model/provider selection;
- timeout;
- retry;
- failover;
- structured output enforcement;
- vision capability routing;
- token/cost metadata;
- correlation IDs;
- task rate limits;
- model allowlist;
- usage telemetry.

Failover must not downgrade to an incompatible model.

Example:

```text
receipt vision route
```

must never fail over to a text-only model.

## 28.3 Suggested Go Interface

```go
type LLMGateway interface {
    Structured(
        ctx context.Context,
        req StructuredRequest,
        out any,
    ) (LLMMetadata, error)
}
```

Example request model:

```go
type StructuredRequest struct {
    RequestID   string
    Task        string
    ModelPolicy string
    SystemPrompt string
    UserContent any
    SchemaName  string
}
```

---

# 29. LLM Security Boundary

All external content is untrusted:

- Telegram messages;
- email content;
- receipt text;
- screenshots;
- merchant names;
- image OCR;
- document text.

Prompt injection inside a receipt or email must be treated as content, never instruction.

Recommended design:

```text
Untrusted source
      |
      v
Quarantine extractor
(no tools)
      |
      v
strict JSON
      |
      v
Go validator
      |
      v
trusted deterministic domain
```

Extractor LLM has:

```text
NO database tool
NO filesystem tool
NO shell
NO arbitrary HTTP
NO secrets
NO mutation tool
```

---

# 30. Data Minimization for LLM Calls

Send only what the model needs.

Examples:

Bad:

```text
entire email history
all transactions for the last year
bank credentials
full account identifiers
```

Good:

```text
normalized current email body
relevant merchant string
single image/document
small candidate category list
precomputed analytics facts
```

Redact:

- credentials;
- private keys;
- PIN;
- CVV;
- passwords;
- unrelated account identifiers;
- unnecessary PII.

---

# 31. Gmail Integration

> Migration note (4 September 2026): Gmail OAuth/Watch/PubSub is temporary
> during the two-deploy Cloudflare email-ingress migration in ADR-033. The
> long-term application ingress is the generated `richmod.link` recipient;
> Gmail remains only as forwarding source after cutover. Retain this section
> as historical behavior until Deploy 2 removes the runtime.

If Gmail is used:

```text
Gmail users.watch
      |
Google Pub/Sub
      |
webhook
      |
history.list
      |
fetch relevant messages
      |
source_event
```

Requirements:

- watch renewal job;
- idempotent Gmail message ingestion;
- process only relevant senders/subjects;
- never scan unrelated mailbox content more broadly than needed;
- keep message IDs for deduplication;
- parser versioning;
- safe reprocessing.

Job:

```text
RENEW_GMAIL_WATCH
```

Webhook should enqueue and return quickly.

---

# 32. Jago Sender Validation

Before parsing a message as trusted Bank Jago evidence, validate available metadata.

At minimum:

```text
expected sender domain
message headers
source mailbox
subject family
```

Where available, retain and inspect email authentication results such as:

```text
DKIM
SPF
DMARC
```

Do not rely on visible `From:` alone if stronger metadata is available.

Unknown or suspicious messages must go to review and must not create financial records.

---

# 33. Jago Parser Versioning

Create parser interface:

```go
type BankEmailParser interface {
    CanParse(msg ParsedEmail) bool
    Parse(ctx context.Context, msg ParsedEmail) (BankEvent, error)
    Name() string
    Version() string
}
```

Example:

```text
jago-v1
```

Do not encode parsing only as fragile global regex.

Parse semantic HTML labels:

```text
Dari
Ke
Jumlah
Tanggal transaksi
Status transaksi
```

Normalize HTML safely.

---

# 34. Unknown Email Template Fallback

If sender is valid Jago but deterministic parser cannot recognize the template:

```text
source_event
    |
    v
UNKNOWN_EMAIL_TEMPLATE
    |
    v
optional Cloud LLM structured extraction
    |
    v
Go validation
    |
    v
Review Inbox
```

Do not silently auto-confirm transactions extracted only by fallback LLM until the template is explicitly supported or confidence policy allows it.

Unknown-template events should be easy to reprocess after a parser update.

---

# 35. API Design

Base:

```text
/api/v1
```

## 35.1 Authentication

```text
POST /auth/login
POST /auth/logout
GET  /auth/me
```

Web sessions use a 24-hour sliding inactivity window. Every successfully
authenticated request extends the session for another 24 hours; an idle session
expires and must sign in again.

## 35.2 Transactions

```text
GET    /transactions
POST   /transactions
GET    /transactions/:id
PATCH  /transactions/:id
POST   /transactions/:id/confirm
POST   /transactions/:id/void
GET    /transactions/:id/evidence
```

Manual income and expense entry creates a confirmed canonical transaction only
alongside a retained `WEB_MANUAL` source event, transaction evidence, and audit
record in one database transaction.

## 35.3 Accounts

```text
GET  /accounts
POST /accounts
PATCH /accounts/:id
```

## 35.4 Categories

```text
GET  /categories
POST /categories
PATCH /categories/:id
```

## 35.5 Merchants

```text
GET  /merchants
POST /merchants
POST /merchants/:id/aliases
```

## 35.6 Source Events

```text
GET  /sources
GET  /sources/:id
POST /sources/:id/reprocess
```

## 35.7 Review

```text
GET  /reviews
GET  /reviews/:id
POST /reviews/:id/confirm
POST /reviews/:id/reject
POST /reviews/:id/merge
POST /reviews/:id/ask-telegram
```

## 35.8 Documents

```text
POST /documents
GET  /documents/:id
GET  /documents/:id/extraction
```

## 35.9 Analytics

```text
GET /analytics/overview
GET /analytics/spending
GET /analytics/cashflow
GET /analytics/categories
GET /analytics/merchants
```

## 35.10 Budgets

```text
GET  /budgets
POST /budgets
PATCH /budgets/:id
```

## 35.11 Insights

```text
GET  /insights
POST /insights/generate
```

---

# 36. Webhooks

```text
POST /webhooks/telegram
POST /webhooks/gmail/pubsub
```

Rules:

- authenticate;
- validate payload;
- idempotency check;
- persist source event;
- enqueue job;
- respond quickly.

Never do long LLM calls synchronously inside webhook handlers.

---

# 37. Job Queue

Suggested `job` table:

```text
id
type
payload_json
status
attempts
max_attempts
run_after
locked_at
locked_by
last_error
created_at
updated_at
```

Statuses:

```text
PENDING
RUNNING
SUCCEEDED
FAILED
DEAD
```

Suggested jobs:

```text
PROCESS_SOURCE_EVENT
PARSE_JAGO_EMAIL
PROCESS_TELEGRAM_MESSAGE
PROCESS_DOCUMENT
CLASSIFY_DOCUMENT
EXTRACT_PAYSLIP
EXTRACT_RECEIPT
EXTRACT_TRANSACTION_SCREENSHOT
CLASSIFY_TRANSACTION
NORMALIZE_MERCHANT
GENERATE_RECONCILIATION_CANDIDATES
SEND_TELEGRAM_REVIEW
PROCESS_TELEGRAM_REVIEW_REPLY
GENERATE_INSIGHTS
RENEW_GMAIL_WATCH
```

Use exponential backoff.

Jobs must be idempotent where possible.

---

# 38. Main Database Tables

MVP target tables:

```text
household
user
household_member
session

account
known_account
known_recipient

category
merchant
merchant_alias

source_event
attachment
document_extraction

transaction_proposal
transaction
transaction_evidence

reconciliation_candidate
reconciliation_event

review_item
review_request
review_conversation

budget

insight

job
audit_log
```

Do not create asset/portfolio tables during MVP.

---

# 39. Audit Log

Every meaningful financial mutation must be audited.

Suggested:

```text
id
household_id
actor_type
actor_id
action
entity_type
entity_id
before_json
after_json
request_id
created_at
```

Actor types:

```text
USER
SYSTEM
EMAIL_PARSER
LLM_SUGGESTION
WORKER
TELEGRAM
```

Important distinction:

`LLM_SUGGESTION` is not authorization.

Actual mutation actor may still be `SYSTEM` or `USER` after validation.

---

# 40. Dashboard Information Architecture

Navigation:

```text
Overview
Transactions
Spending
Budgets
Review Inbox
Insights
Settings
```

No Assets/Portfolio navigation in MVP.

## 40.1 Overview

Cards:

```text
Income this month
Expense this month
Net cashflow
Savings rate
Review count
```

Charts:

```text
cashflow trend
spending by category
spending trend
top merchants
spending by household member
```

## 40.2 Transaction Page

Display:

- amount;
- type;
- date;
- merchant;
- category;
- note;
- account;
- status;
- source badges;
- evidence timeline;
- audit/corrections where appropriate.

Example provenance:

```text
Rp55,199
PAMELLA DUA
Household > Groceries

Evidence
✓ Bank Jago email
✓ Telegram clarification
✓ Receipt
```

---

# 41. Analytics Rules

All analytics must be deterministic.

## 41.1 Income

```text
sum(CONFIRMED INCOME)
```

## 41.2 Expense

```text
sum(CONFIRMED EXPENSE)
```

## 41.3 Net Cashflow

```text
income - expense
```

Transfers excluded.

## 41.4 Savings Rate

```text
(income - expense) / income
```

If income <= 0, return unavailable rather than dividing incorrectly.

## 41.5 Refund

Refund treatment should be explicit.

Recommended initial approach:

```text
REFUND reduces net spending
```

Keep original expense and refund separately for audit.

---

# 42. LLM Insights

Do not send raw transaction history to the LLM and ask it to calculate finance metrics.

Instead:

```text
PostgreSQL
    |
    v
Go analytics
    |
    v
structured facts
    |
    v
Cloud LLM Gateway
    |
    v
narrative insight
```

Example input:

```json
{
  "period": "2026-08",
  "income": "16000000",
  "expense": "7300000",
  "savings_rate": "0.54375",
  "category_changes": [
    {
      "category": "Dining",
      "change_vs_3m_average": "0.18"
    }
  ],
  "data_completeness": 0.96
}
```

Example output:

```text
Pengeluaran makan bulan ini naik sekitar 18%
dibanding rata-rata tiga bulan sebelumnya.
```

Store:

```text
period
input_metrics_json
gateway_route
provider
model
prompt_version
generated_text
confidence
data_completeness
created_at
```

Do not generate confident recommendations when completeness is poor.

---

# 43. Security

Required:

- TLS;
- secure cookies;
- CSRF protection where relevant;
- Argon2id password hashing;
- session expiration;
- rate limiting;
- security headers;
- CSP;
- least-privilege DB user;
- no public PostgreSQL;
- no public worker;
- file upload validation;
- secret redaction;
- audit log;
- encrypted backups;
- access control by household;
- webhook secrets;
- dependency scanning;
- secret scanning.

Never store:

```text
PIN
CVV
bank password
broker password
private key
OTP
```

If a user sends credentials accidentally through Telegram:

```text
detect
do not persist if avoidable
redact logs
warn user
```

---

# 44. Docker / Network Isolation

Suggested:

```text
finance_edge
finance_internal
```

Flow:

```text
reverse proxy
    |
finance_edge
    |
web/api
    |
finance_internal
    |
db/worker
```

PostgreSQL must not expose a public host port in production.

Suggested initial container limits:

```text
finance-web     512 MB
finance-api     768 MB
finance-worker  1 GB
finance-db      1 GB
```

Tune after observing real usage.

---

# 45. Secrets

Expected secrets:

```text
DATABASE_URL / DB password
SESSION_SECRET
TELEGRAM_BOT_TOKEN
TELEGRAM_WEBHOOK_SECRET
GMAIL OAuth credentials / refresh token
ENCRYPTION_MASTER_KEY
LLM_GATEWAY_BASE_URL
LLM_GATEWAY_API_KEY
BACKUP_ENCRYPTION_KEY
```

Never commit secrets.

Prefer:

- mounted secrets;
- Docker secrets where practical;
- restricted file permissions;
- environment injection only where acceptable.

---

# 46. Backups

Daily encrypted off-host backup.

Suggested retention:

```text
daily   14
weekly   8
monthly 12
```

Production acceptance requires at least one tested restore procedure.

Backup without restore testing is not considered complete.

---

# 47. Observability

Required:

- structured logs;
- request correlation ID;
- job correlation ID;
- health endpoint;
- readiness endpoint;
- worker heartbeat;
- job backlog;
- job failure count;
- Jago parser failure count;
- unknown-template count;
- Gmail sync freshness;
- Telegram delivery failures;
- review backlog;
- LLM gateway latency;
- LLM usage/cost metadata;
- backup freshness.

Future Prometheus/Grafana is allowed but not mandatory for initial MVP.

---

# 48. Data Retention

Suggested policy:

```text
canonical transactions  indefinite
audit log               indefinite
transaction evidence    long-term
receipts/images          configurable
raw email payload        configurable shorter retention
LLM transient payloads   minimize and expire where possible
```

Do not retain raw external data merely because it is available.

---

# 49. Feature Flags

Recommended:

```text
FEATURE_TELEGRAM
FEATURE_GMAIL
FEATURE_JAGO_EMAIL
FEATURE_DOCUMENT_VISION
FEATURE_PAYSLIP
FEATURE_RECEIPT
FEATURE_AUTO_RECONCILIATION
FEATURE_TELEGRAM_REVIEW
FEATURE_BUDGETS
FEATURE_INSIGHTS
```

Feature flags should support safe rollout and rollback.

---

# 50. Prompt Versioning

Prompts must be files in source control.

Each call records prompt version.

Required prompts:

```text
finance_intent_v1.md
telegram_transaction_extract_v1.md
document_classify_v1.md
payslip_extract_v1.md
receipt_extract_v1.md
transaction_screenshot_extract_v1.md
category_classify_v1.md
merchant_normalize_v1.md
finance_insight_v1.md
```

Prompt changes should be regression-tested.

---

# 51. Testing Strategy

## 51.1 Unit Tests

Required:

- IDR amount parsing;
- decimal handling;
- timestamp parsing;
- Asia/Jakarta relative dates;
- Jago subject-family classifier;
- Jago HTML parser;
- Jago `SPENDING_ONLY` rules;
- own-account transfer detection;
- merchant normalization;
- category rules;
- reconciliation score;
- duplicate handling;
- transaction status transitions;
- savings-rate formula;
- refund handling;
- review reply binding;
- payslip arithmetic validation.

## 51.2 Integration Tests

Required scenarios:

### Jago merchant payment

```text
email -> parse -> expense
```

### Jago incoming

```text
email -> parse -> NO INCOME
```

### Jago internal pocket transfer

```text
email -> NO EXPENSE
```

### Jago external transfer

```text
email -> expense candidate -> Telegram clarification
```

### Telegram manual expense

```text
message -> LLM extraction -> validation -> transaction
```

### Review reply

```text
bot asks -> user replies -> exact transaction resolved
```

### Receipt enrichment

```text
Jago expense -> receipt image -> same transaction
```

### Payslip income

```text
image -> extraction -> validation -> income
```

### Duplicate Telegram + Jago

```text
two evidence events -> one transaction
```

## 51.3 Security Tests

Required:

- unauthorized Telegram user;
- forged webhook;
- replayed webhook;
- prompt injection in receipt;
- prompt injection in email;
- prompt injection in merchant name;
- oversized upload;
- invalid MIME;
- cross-household access;
- SQL injection;
- XSS;
- secret logging;
- malicious filename;
- unknown fake Jago sender.

---

# 52. Acceptance Criteria

Current completion evidence and known gaps are maintained in
[`MVP_COMPLETION_CHECKLIST.md`](MVP_COMPLETION_CHECKLIST.md). The criteria below
remain normative; a roadmap phase heading does not itself indicate completion.

## 52.1 Foundation

Complete when:

- containers start;
- DB migrations work;
- health/readiness work;
- secrets are externalized;
- auth works;
- tests run in CI.

## 52.2 Core Ledger

Complete when:

- account/category/merchant CRUD works;
- manual income/expense works;
- Decimal/NUMERIC used;
- evidence attached;
- audit log recorded;
- transactions never hard-delete.

## 52.3 Jago Email

Complete when:

- Gmail watch receives events;
- Jago messages idempotently become source events;
- known Jago payment template parses deterministically;
- incoming Jago money does not become income;
- internal Jago movement does not become expense;
- external ambiguous transfer goes to review;
- parser fixtures exist;
- reprocessing works.

## 52.4 Telegram

Complete when:

- authorized users can send finance messages;
- unauthorized users receive no data;
- natural-language expense works;
- image upload works;
- finance-only intent gate works.

## 52.5 Telegram Review

Complete when:

- ambiguous transaction causes bot push;
- review message maps to exact transaction;
- reply uses `reply_to_message_id`;
- LLM returns structured clarification;
- Go validates before update;
- mutation audited;
- review resolves;
- multiple simultaneous review messages remain unambiguous.

## 52.6 Payslip

Complete when:

- payslip can be sent as image;
- vision classifies it;
- structured extraction works;
- net pay is validated;
- canonical income created only through deterministic handler;
- deduction breakdown does not automatically become household spending.

## 52.7 Receipt

Complete when:

- receipt image validated;
- extraction structured;
- arithmetic validated;
- duplicate transaction avoided;
- existing expense can gain receipt evidence.

## 52.8 Reconciliation

Complete when:

- candidate generation deterministic;
- scoring tested;
- only unambiguous high-confidence matches auto-link;
- ambiguous candidates go to review;
- evidence retained;
- merge reversible.

## 52.9 Dashboard

Complete when:

- monthly income;
- monthly expense;
- net cashflow;
- savings rate;
- category spending;
- top merchants;
- Review Inbox;
- transaction provenance;
- all values come from deterministic DB queries.

## 52.10 Production

Complete when:

- TLS;
- private DB;
- backups;
- restore tested;
- rate limits;
- logs;
- alerts/monitoring basics;
- container restart behavior;
- secrets not committed;
- security regression tests pass.

---

# 53. Development Roadmap

## Phase 0 — Bootstrap [P0]

- repository;
- Go modules;
- Next.js JS app;
- PostgreSQL;
- Docker Compose;
- config;
- migration tooling;
- CI.

## Phase 1 — Core Ledger [P0]

- household;
- users/auth;
- accounts;
- categories;
- merchants;
- source events;
- transactions;
- transaction evidence;
- audit log;
- manual CRUD.

## Phase 2 — Minimal Dashboard [P0]

- overview metrics;
- transaction list;
- category breakdown;
- Review Inbox skeleton.

## Phase 3 — Telegram Manual Input [P0]

- bot webhook;
- authorization;
- finance intent gate;
- Cloud LLM Gateway;
- natural-language transaction extraction.

## Phase 4 — Bank Jago Automation [P0]

- Gmail integration;
- deterministic Jago parser;
- `SPENDING_ONLY` rules;
- sender validation;
- fixtures;
- reprocessing.

## Phase 5 — Reconciliation [P0]

- proposal layer;
- candidate generator;
- score;
- auto-link policy;
- merge/reject.

## Phase 6 — Telegram Interactive Review [P0]

- review request;
- bot push;
- reply binding;
- multi-turn clarification;
- category confirmation;
- merchant learning.

## Phase 7 — Generic Image Intake [P1]

- image upload;
- secure storage;
- classification;
- Cloud vision routing.

## Phase 8 — Payslip [P1]

- extraction;
- income proposal;
- validation;
- dashboard income.

## Phase 9 — Receipt / Screenshot [P1]

- receipt extraction;
- screenshot transaction extraction;
- enrichment;
- reconciliation.

## Phase 10 — Budgets and Analytics [P1]

- budgets;
- category trends;
- merchant trends;
- household member view.

## Phase 11 — LLM Insights [P2]

- structured analytics facts;
- insight generation;
- confidence/completeness.

## Phase 12 — Production Hardening [P0 before long-term use]

- backups;
- restore test;
- security;
- observability;
- resource limits;
- failure recovery;
- runbooks.

---

# 54. Exact Initial Codex Development Order

Codex should execute this order unless an ADR explicitly changes it:

```text
1. scaffold repository
2. docker compose: PostgreSQL + API + web + worker
3. config/secrets abstraction
4. goose migrations
5. sqlc setup
6. household/user/auth
7. account
8. category
9. merchant + merchant_alias
10. source_event
11. transaction
12. transaction_evidence
13. audit_log
14. manual transaction CRUD
15. deterministic dashboard queries
16. baseline unit/integration tests
17. Telegram webhook/auth
18. Cloud LLM Gateway client
19. Telegram finance intent gate
20. Telegram manual transaction extraction
21. Gmail integration
22. Jago deterministic parser
23. Jago SPENDING_ONLY policy
24. transaction_proposal
25. reconciliation
26. Review Inbox
27. Telegram review push/reply
28. merchant/category learning
29. generic document/image intake
30. payslip extraction
31. receipt extraction
32. transaction screenshot extraction
33. budgets
34. advanced analytics
35. LLM insights
36. production hardening
```

Do not start image vision before the core transaction ledger and evidence model are stable.

---

# 55. Codex Working Rules

Codex MUST:

1. treat this document as the source of truth;
2. implement Go backend only;
3. use JavaScript for web application code;
4. use PostgreSQL as canonical financial store;
5. use Decimal/NUMERIC for money;
6. keep DB private;
7. preserve evidence;
8. never hard-delete canonical finance records;
9. put untrusted extractions into proposals;
10. validate all LLM results deterministically;
11. never allow LLM direct DB access;
12. route all model calls through Cloud LLM Gateway;
13. never instantiate provider-specific model SDKs in finance business logic;
14. implement deterministic Jago parser before LLM fallback;
15. apply Jago `SPENDING_ONLY` policy;
16. never count normal incoming Jago funding as household income;
17. treat internal transfer as non-spending;
18. bind Telegram replies through message/review IDs;
19. prefer human review over silent ambiguity;
20. add audit logs for meaningful mutations;
21. make webhooks idempotent;
22. add tests before marking an issue complete;
23. update ADR/docs when introducing architectural changes;
24. keep system functional when LLM gateway is unavailable;
25. never add Redis/Kafka/vector DB without ADR;
26. never implement assets/portfolio during MVP;
27. never expose general-purpose agent tools.

---

# 56. Definition of Done for Every Feature

A feature is done only when:

- schema/migration exists where required;
- domain rules implemented;
- API/webhook implementation complete;
- authorization implemented;
- idempotency considered;
- error handling implemented;
- audit implemented for mutation;
- structured logging implemented;
- unit tests added;
- integration tests added when applicable;
- failure behavior documented;
- docs updated;
- no secrets in logs;
- no cross-household access;
- deterministic validation exists for any LLM output.

---

# 57. Migration Policy

Production migrations are forward-only.

For destructive change:

```text
1. add replacement
2. dual-read/write if needed
3. backfill
4. switch
5. observe
6. remove old field/table in later release
```

Never drop financial data in the same deploy that introduces replacement logic.

---

# 58. Rollback Strategy

Use:

- feature flags;
- backward-compatible migrations;
- prompt versioning;
- parser versioning;
- reprocessable source events.

A parser deployment must be reversible without losing original evidence.

---

# 59. Failure Scenarios

## LLM gateway unavailable

Behavior:

```text
deterministic flows continue
Jago known-template parsing continues
manual structured operations continue
LLM-dependent work -> pending/review
```

Do not fail the entire system.

## Telegram unavailable

Behavior:

```text
transactions continue ingesting
review items accumulate
web Review Inbox remains available
```

## Gmail unavailable

Behavior:

```text
manual/Telegram flows continue
freshness indicator becomes stale
sync resumes idempotently
```

## Unknown Jago template

Behavior:

```text
retain source_event
review/fallback extraction
no blind auto-post
```

---

# 60. Example End-to-End Journeys

## 60.1 Automatic Known Merchant

```text
Jago email:
SUPERINDO Rp320,000
       |
Jago parser
       |
merchant alias:
SUPERINDO -> Household > Groceries
       |
canonical EXPENSE
       |
no Telegram interruption
```

## 60.2 Unknown Merchant Purpose

```text
Jago email:
PAMELLA DUA Rp55,199
       |
expense candidate
       |
category uncertain
       |
Telegram push
       |
"Ini buat apa?"
       |
User:
"belanja bulanan, sabun dan kebutuhan dapur"
       |
LLM -> structured clarification
       |
Go validation
       |
Household > Groceries
       |
CONFIRMED
```

## 60.3 Generic Transfer

```text
Jago transfer:
Rp450,000 -> BUDI
       |
recipient unknown
       |
NO auto classification
       |
Telegram:
"Transfer Rp450.000 ke Budi tadi untuk apa?"
       |
User:
"patungan hotel"
       |
Travel > Accommodation
```

## 60.4 Incoming Jago Funding

```text
Jago incoming:
Rp1,000,000
       |
SPENDING_ONLY policy
       |
source retained
       |
NO INCOME
NO EXPENSE
```

## 60.5 Payslip

```text
Telegram image
       |
PAYSLIP
       |
vision extraction
       |
net pay = Rp16,000,000
       |
Go validation
       |
INCOME
       |
dashboard updated
```

## 60.6 Receipt After Jago Payment

```text
Jago expense Rp185,000
       |
canonical transaction

Later:
receipt photo Rp185,000
       |
vision extraction
       |
reconciliation
       |
same transaction
       |
receipt becomes evidence
```

---

# 61. Explicit Non-Goals

The following are not goals of the MVP:

- replacing accounting software;
- formal tax filing;
- bank balance scraping;
- banking credential automation;
- executing financial transactions;
- executing investments;
- financial portfolio management;
- supporting every bank;
- general-purpose chat assistant;
- autonomous financial decision-making;
- vector-memory knowledge agent;
- OCR infrastructure independent of vision models unless later justified.

---

# 62. Future Extension Points

After MVP is stable, possible modules:

```text
Asset / Investment
Additional bank adapters
Android notification adapter
More automated account reconciliation
Household shared budgets
Recurring bill detection
Cash-flow forecasting
Financial goals
```

Future modules must not compromise current ledger/evidence/audit guarantees.

---

# 63. Architectural Decisions to Record as ADRs

Create ADRs for:

```text
ADR-001 Go backend
ADR-002 JavaScript Next.js frontend
ADR-003 PostgreSQL job queue
ADR-004 Evidence-first canonical ledger
ADR-005 Cloud LLM Gateway boundary
ADR-006 Bank Jago SPENDING_ONLY account policy
ADR-007 Deterministic Jago parser + LLM fallback
ADR-008 Telegram human-in-the-loop review
ADR-009 Generic finance document intake
ADR-010 Transaction proposal before untrusted mutation
ADR-011 Receipt/image storage strategy
ADR-012 Reconciliation scoring policy
```

---

# 64. Final Architecture Principle Summary

The implementation should continuously enforce these principles:

```text
Evidence over deletion.
Deterministic rules over model guesses.
Human clarification over silent ambiguity.
Structured LLM output over free-form mutation.
Go owns financial state transitions.
Cloud gateway over provider coupling.
Known Jago templates over LLM-first email parsing.
Jago outgoing spending over Jago balance tracking.
Incoming Jago funding is not household income.
Telegram is both input channel and interactive Review Inbox.
Replies bind to exact transactions through message IDs.
Simple deployment over premature distributed architecture.
Audit every meaningful financial mutation.
The system must still work when the LLM is unavailable.
Assets are explicitly out of MVP scope.
```

---

# 65. Codex Starting Instruction

When beginning implementation:

1. read this document completely;
2. do not start with Telegram, Gmail, or LLM;
3. build and test the canonical ledger first;
4. implement evidence/provenance before external automation;
5. add Jago integration only after ledger is stable;
6. build Telegram review after reconciliation exists;
7. only then add generic image/payslip/receipt flows;
8. keep changes small and testable;
9. create migrations and tests with each domain feature;
10. do not invent requirements outside this specification.

If implementation constraints require a deviation, create an ADR and explain the tradeoff before changing the architecture.
