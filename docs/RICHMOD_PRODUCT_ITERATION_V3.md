# Richmod Product Alignment v3.0 Final — Salary Cycle, Fast Telegram Interactions & Multi-Image Documents

**Document type:** Iteration / implementation plan for Codex  
**Repository:** `raufimusaddiq/richmod`  
**Status:** Supersedes Product Alignment v3/v3.1 where this document conflicts.

> This is an iteration snapshot, not a permanent source of truth. The latest explicit user instruction and current repository state take precedence. Update living repository docs as implementation evolves.

---

# 1. Product direction for this iteration

Richmod is a household finance tracker and financial assistant focused on:

```text
TRACK
  ↓
UNDERSTAND
  ↓
REVIEW WHEN NEEDED
  ↓
ANALYZE
```

Budgeting is not an active product focus.

The main new product capabilities in this iteration are:

1. **fast Telegram interactions for every inline button**;
2. **multiple images as one logical financial document**;
3. **payslip-driven income and dynamic salary-cycle periods**;
4. **universal review and correct member routing**;
5. **cycle-aware analytics, Telegram queries, and insights**;
6. **professional Indonesian product copy and UI polish**.

---

# 2. Salary source scope correction

The salary bank account is **not ingested** in the current product scope.

Salary enters Richmod from payslip images uploaded through Telegram or web.

Bank Jago remains the household spending account.

```text
Salary bank account
    │
    │ not ingested by Richmod
    │
    ├── payslip image(s)
    │       ↓
    │   salary income
    │   salary event
    │   salary-cycle boundary
    │
    └── transfer money to Jago
            ↓
       Jago incoming funding
            ↓
          IGNORE

Jago outgoing actual spending
            ↓
          EXPENSE
```

Therefore, current scope explicitly defers:

```text
PDF payslips
salary-bank Gmail adapter
salary-credit detection
salary-bank transaction ingestion
payslip-to-bank-credit reconciliation
```

Do not reinterpret incoming Bank Jago transfers as household income.

---

# 3. Priority order

```text
P0  Telegram Interaction Latency Architecture
P0  Multi-Image Logical Documents
P0  Universal Review Item
P0  Deterministic Review Recipient Routing
P0  Salary Source + Salary Event
P0  Payslip-Driven Dynamic Salary Cycle

P1  Cycle-Aware Ringkasan / Analisis / Telegram / Insights
P1  Professional Indonesian Product Copy
P1  Salary-Cycle UI + Chart Polish

P2  Invoice Flow
P2  Known-Account Hardening
P2  Encrypted Off-Host Backup
```

**Telegram Interaction Latency is independent from image processing.**
It applies to all inline buttons throughout the bot.

---

# 4. P0 — Telegram Interaction Latency Architecture

## 4.1 Problem

Telegram inline buttons currently pass through too much asynchronous infrastructure before Telegram receives callback acknowledgement.

This affects buttons such as:

```text
Konfirmasi
Ubah
Pengeluaran
Rekening sendiri
Rekening keluarga
Kategori
Ingat merchant
Hanya kali ini
Abaikan
Payslip confirmation
Future review actions
```

The problem is not specific to payslips or image uploads.

Interactive user actions must not be delayed by:

```text
LLM extraction
vision/document processing
Gmail processing
insight generation
image download
other background jobs
```

## 4.2 Required fast path

Required architecture:

```text
User taps Telegram inline button
        ↓
Telegram webhook
        ↓
validate webhook secret
        ↓
validate private chat
        ↓
validate callback action
        ↓
authorize numeric Telegram identity
        ↓
idempotently persist callback event + callback job
        ↓
answerCallbackQuery("Diproses…") IMMEDIATELY
        ↓
HTTP 204

        asynchronous
             ↓
INTERACTIVE worker lane
             ↓
PROCESS_TELEGRAM_CALLBACK
             ↓
deterministic callback handler
             ↓
DB transaction / review transition
             ↓
edit original Telegram message
             ↓
remove obsolete buttons
```

`answerCallbackQuery` is only transport acknowledgement.

It does **not** mean the financial mutation succeeded.

## 4.3 Callback acknowledgement requirements

The webhook/API layer may use the Telegram bot token only for narrow transport operations such as:

```text
answerCallbackQuery
```

Recommended sequence:

```text
persist callback durably
→ ACK Telegram
→ return webhook response
```

If ACK fails after persistence:

```text
DO NOT roll back persisted callback
log redacted transport failure
continue asynchronous processing
```

## 4.4 Dedicated callback job

Add:

```text
PROCESS_TELEGRAM_CALLBACK
```

Do not convert button callback values into artificial user text.

Do not send deterministic button actions through the general Telegram LLM intent pipeline.

Examples:

```text
review:confirm
    → ConfirmReview()

review:own
    → ClassifyTransfer(OWN_ACCOUNT)

review:household
    → ClassifyTransfer(HOUSEHOLD_ACCOUNT)

review:expense
    → TransitionReview(AWAITING_CATEGORY)

review:category:groceries
    → SetReviewCategory("groceries")

review:remember
    → EnableMerchantAutoRule()

review:once
    → ResolveWithoutMerchantRule()

review:ignore
    → IgnoreReview()
```

Button actions must be deterministic and require **zero LLM calls** unless the button explicitly starts an AI-heavy operation.

Natural-language replies remain on the existing text/LLM path.

## 4.5 Separate interactive execution lane

Do not let long-running background work monopolize callback processing.

Use explicit job lanes:

```text
INTERACTIVE
DEFAULT
BACKGROUND
```

Suggested classification:

```text
INTERACTIVE
───────────
PROCESS_TELEGRAM_CALLBACK
SEND_TELEGRAM_MESSAGE
EDIT_TELEGRAM_MESSAGE
PROCESS_TELEGRAM_REVIEW_TEXT

DEFAULT
───────
PROCESS_TELEGRAM_TEXT
PROCESS_GMAIL_HISTORY

BACKGROUND
──────────
FETCH_TELEGRAM_IMAGE
FINALIZE_TELEGRAM_MEDIA_GROUP
PROCESS_DOCUMENT
PROCESS_PAYSLIP
PROCESS_RECEIPT
PROCESS_TRANSACTION_SCREENSHOT
GENERATE_INSIGHT
```

Run at least one execution loop reserved for `INTERACTIVE` jobs.

A long vision request must not block Telegram buttons.

Example:

```text
BACKGROUND worker
PROCESS_PAYSLIP → vision request takes 12 sec

INTERACTIVE worker
review:confirm → processed immediately
```

Do not rely only on priority sorting if all jobs are still executed by one serial worker.

## 4.6 Telegram message editing

Add Bot API support for:

```text
answerCallbackQuery
editMessageText
editMessageReplyMarkup
sendMessage
```

For inline review actions, prefer editing the original bot message.

Before:

```text
Transfer Rp1.000.000 ke BCA ****1234

Jenis transfer ini?

[ Pengeluaran ]
[ Rekening sendiri ]
[ Rekening keluarga ]
```

After tapping `Rekening sendiri`:

```text
✓ Transfer dikonfirmasi

Rp1.000.000 → BCA ****1234
Transfer antar rekening sendiri
Tidak dihitung sebagai pengeluaran.
```

Remove obsolete buttons.

Do not create a new bot message for every button interaction unless an edit is not possible.

If message edit fails after the financial DB mutation commits:

```text
DO NOT roll back financial state
queue retryable notification repair
fallback to sendMessage if appropriate
```

## 4.7 Performance targets

Under normal network conditions:

```text
callback ACK after webhook receipt:       < 500 ms p95
perceived Telegram spinner duration:      < 1 second
simple deterministic mutation after pick: < 1 second p95
updated Telegram message:                 < 2 seconds p95
```

These are product latency objectives, not transaction correctness shortcuts.

## 4.8 Callback safety

Required:

```text
numeric Telegram identity authorization
webhook secret validation
private-chat enforcement
callback allowlist validation
exact review/message binding
household scope validation
idempotent Telegram update handling
reject stale/resolved actions
financial mutation audit log
no duplicate mutation on callback replay
```

Callback ACK must never be used as mutation authorization.

---

# 5. P0 — Multi-Image Logical Financial Documents

## 5.1 Supported file types

Current scope supports only:

```text
image/jpeg
image/png
```

No PDF support is required.

## 5.2 Product behavior

One logical document may contain multiple ordered images.

Primary use case:

```text
Payslip screenshot 1
Payslip screenshot 2
Payslip screenshot 3
        ↓
ONE logical document
        ↓
ONE document classification
        ↓
ONE payslip extraction
        ↓
ONE salary income candidate
```

Never interpret each page/image as an independent payslip transaction.

## 5.3 Telegram album support

Telegram albums send multiple message updates with the same:

```text
media_group_id
```

Parse and preserve:

```text
media_group_id
message_id
update_id
caption
telegram_user_id
```

Each Telegram update remains its own immutable `source_event` for provenance.

The shared `media_group_id` determines the logical document grouping.

## 5.4 Document model

`document` should represent one logical financial document.

Add an additive mapping table such as:

```sql
CREATE TABLE document_page (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES document(id),
    source_event_id UUID NOT NULL REFERENCES source_event(id),
    attachment_id UUID NOT NULL REFERENCES attachment(id),
    page_index INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(document_id, page_index),
    UNIQUE(document_id, source_event_id)
);
```

Existing single-page document fields may remain temporarily for backward compatibility.

## 5.5 Telegram album intake flow

```text
Telegram album
   ↓
multiple Telegram webhook updates
   ↓
each update creates TELEGRAM_IMAGE source_event
   ↓
each image downloaded and normalized independently
   ↓
attachments persisted
   ↓
group by household + sender + media_group_id
   ↓
short debounce/finalization
   ↓
ONE document
   ↓
ordered document_page rows
   ↓
PROCESS_DOCUMENT once
```

Recommended debounce:

```text
1–3 seconds after latest album item
```

Album arrival order may differ from update processing order.

Use deterministic order based on Telegram message/page metadata.

## 5.6 Non-album images

If no `media_group_id` exists:

```text
one image → one logical document
```

Do not guess that sequential standalone images belong together.

## 5.7 Web multiple-image upload

Allow one upload submission with multiple JPEG/PNG images:

```html
<input type="file" multiple accept="image/jpeg,image/png">
```

One submission creates one logical document with ordered pages.

## 5.8 Limits

Keep bounded configurable limits.

Initial defaults may be:

```text
max images/document: 10
max bytes/image: existing safe image limit
max combined normalized bytes: bounded config
max combined pixels: bounded config
```

Reject invalid/oversized bundles before model processing.

## 5.9 LLM vision calls

Classifier gets all pages in a single request:

```text
input_image page 1
input_image page 2
input_image page 3
...
```

Document-specific extraction then receives the same logical document pages.

Payslip extraction prompt must treat the pages as parts of one payslip.

If pages clearly belong to different financial documents:

```text
NEEDS_REVIEW
```

Do not merge conflicting employers, totals, or payroll periods.

---

# 6. P0 — Universal Review Item

Current review architecture must support ambiguity before a canonical transaction exists.

Create/use one universal review entity capable of referring to:

```text
transaction
transaction proposal
source event
document
```

Typical review types:

```text
AMBIGUOUS_CATEGORY
UNKNOWN_PURPOSE
POSSIBLE_DUPLICATE
TRANSFER_CLASSIFICATION
DOCUMENT_CLASSIFICATION
PAYSLIP_CONFIRMATION
MISSING_PAY_DATE
SALARY_SOURCE_CONFIRMATION
UNKNOWN_BANK_TEMPLATE
INVOICE_PAYMENT_STATUS
MANUAL_CORRECTION
```

Telegram and web must resolve the same underlying review item.

Do not maintain duplicate independent review state.

---

# 7. P0 — Deterministic Review Recipient Routing

With multiple household users, never send a sensitive review to an arbitrary Telegram identity.

Do not use behavior equivalent to:

```text
ORDER BY created_at LIMIT 1
```

Centralize routing.

Suggested priority:

```text
1. review.preferred_user_id
2. salary_source.user_id for salary/payslip review
3. source uploader / creator
4. account owner where deterministically known
5. household OWNER fallback
6. no valid Telegram recipient → web review only
```

Payslip amount/employer information must not be sent to an unrelated member just because that member linked Telegram first.

---

# 8. P0 — Payslip-Driven Salary Source

Salary enters Richmod through one or more payslip images.

Extract:

```text
period
employer
gross_pay
allowances[]
deductions[]
net_pay
currency
pay_date
confidence
```

Rules:

```text
net_pay = candidate household income
payroll deductions = metadata only
payslip never creates household expenses from deductions
LLM extraction must pass Go validation
```

## 8.1 First salary source

If no primary salary source exists:

```text
Slip gaji Agustus berhasil dibaca.

Gaji bersih: Rp16.000.000
Tanggal pembayaran: 25 Agustus

Gunakan sebagai acuan siklus keuangan?

[ Ya, gaji utama ]
[ Pemasukan biasa ]
[ Abaikan ]
```

`Ya, gaji utama` creates/links:

```text
canonical INCOME
salary_source
salary_event NORMAL_SALARY CONFIRMED
```

and makes that salary source the primary household cycle anchor.

`Pemasukan biasa` creates income without changing the cycle anchor.

## 8.2 Subsequent payslips

For a known salary source:

```text
same salary source
unique payroll period
valid net pay
valid/matched pay date
sufficient extraction confidence
```

may reduce review friction.

Prevent duplicate income for the same salary source + payroll period.

## 8.3 Missing pay date

Never silently use expected payday as actual pay date.

Ask the correct user:

```text
Aku belum menemukan tanggal pembayaran di slip ini.
Gaji masuk tanggal berapa?
```

The expected payday may only appear as a suggestion.

---

# 9. P0 — Dynamic Salary Cycle

Create/keep:

```text
salary_source
salary_event
financialperiod resolver
```

Only confirmed:

```text
NORMAL_SALARY
+
active primary salary source
```

may create a salary-cycle boundary.

Example:

```text
Payslip Aug:
pay_date = 25 Aug

Payslip Sep:
pay_date = 24 Sep

Cycle A = [25 Aug, 24 Sep)
Cycle B = [24 Sep, next confirmed salary event)
```

Expected payday such as the 25th is prediction only.

Do not open or close a financial cycle because the calendar reaches the expected date.

The current cycle remains open until the next confirmed salary event.

Non-anchor income does not reset the cycle, including:

```text
bonus
THR
refund
cashback
reimbursement
side income
non-anchor spouse salary
```

---

# 10. P1 — Cycle-Aware Product Behavior

When a primary salary source exists:

```text
Ringkasan default period = CURRENT_CYCLE
```

Without salary setup:

```text
Ringkasan default period = CURRENT_CALENDAR_MONTH
```

Analytics must support:

```text
[ Siklus Gaji ] [ Kalender ]
```

Telegram period intents must support:

```text
CURRENT_CYCLE
PREVIOUS_CYCLE
THIS_MONTH
LAST_MONTH
TODAY
THIS_WEEK
LAST_WEEK
CUSTOM
```

Examples:

```text
"berapa pengeluaranku?"
→ CURRENT_CYCLE when configured

"sejak gajian terakhir?"
→ CURRENT_CYCLE

"siklus sebelumnya?"
→ PREVIOUS_CYCLE

"bulan Agustus?"
→ calendar August
```

LLM extracts intent/period meaning.

Go resolves exact date boundaries and calculates authoritative financial values.

---

# 11. P1 — Cycle-Aware Insights

Insights must receive deterministic precomputed facts.

Include:

```text
period kind
period start
period end/open
income
expense
net cashflow
category distribution
merchant distribution
member attribution
previous-cycle comparison
review count
data completeness
```

LLM generates narrative only.

It must not calculate authoritative totals from raw transactions.

---

# 12. P1 — Professional Indonesian Product Copy

Copy improvement is a required deliverable.

Visible primary navigation:

```text
Ringkasan
Transaksi
Analisis
Perlu Ditinjau
Dokumen
Keluarga
Pengaturan
```

Remove primary UI terminology such as:

```text
Overview
Analytics
Ledger
Review Inbox
Household
canonical
proposal
evidence
classification confidence
```

Technical terms may remain in an optional technical/provenance detail section.

Tone:

```text
professional
calm
clear
financially literate
not robotic
not gamified
```

Use consistent terminology between web and Telegram.

Examples:

```text
"Evidence"     → "Sumber pencatatan"
"Review Inbox" → "Perlu Ditinjau"
"Ledger"       → "Riwayat transaksi"
"Household"    → "Keluarga"
```

---

# 13. P1 — UI and Chart Polish

Keep the current routed app architecture.

Do not rewrite the frontend architecture again solely for aesthetics.

Improve:

```text
Recharts line/area charts
salary-cycle labels
professional tooltips
consistent typography
responsive layout
subtle chart animation
better loading skeletons
clean empty states
transaction provenance detail
review-state clarity
```

Avoid:

```text
generic SaaS admin styling
crypto dashboard aesthetics
excessive gradients
large low-information cards
budget-first visual language
```

Ringkasan should clearly show:

```text
Siklus berjalan
25 Agu – sekarang

Pemasukan
Pengeluaran
Sisa arus kas
Perlu ditinjau
```

Optional salary information:

```text
Gaji terakhir
Perkiraan gajian berikutnya
```

Estimates must be visually labelled as estimates.

---

# 14. Telegram Callback Acceptance Tests

Required:

```text
confirm button
→ callback persisted once
→ callback ACK attempted immediately
→ deterministic mutation
→ original message updated
→ buttons removed

category button
→ zero LLM calls

own-account transfer button
→ zero LLM calls
→ transaction becomes TRANSFER
→ excluded from spending

callback while PROCESS_PAYSLIP is running
→ ACK remains fast
→ interaction is not blocked by vision worker

same callback replayed
→ mutation occurs once

resolved review button pressed again
→ no duplicate mutation
→ concise already-resolved response

unauthorized Telegram callback
→ no household data exposure
→ no mutation

natural-language reply
→ existing text/LLM clarification path remains available
```

Add latency-oriented integration tests/metrics where practical.

---

# 15. Multi-Image Acceptance Tests

Required:

```text
single Telegram photo
→ one source event
→ one logical document

Telegram album with 3 images
→ 3 source events
→ 3 attachments/pages
→ ONE logical document
→ ONE classifier call
→ ONE payslip extraction

album updates out of order
→ deterministic page order

replayed album update
→ no duplicate page

same binary image appears twice
→ attachment dedup safe
→ source provenance preserved

standalone images sent separately
→ separate documents

web submission with 3 images
→ one document with 3 pages

mixed/conflicting pages
→ NEEDS_REVIEW
```

---

# 16. Salary-Cycle Acceptance Tests

Required:

```text
25 Aug confirmed payslip
→ cycle starts 25 Aug

24 Sep confirmed payslip
→ previous cycle closes at 24 Sep
→ new cycle starts 24 Sep

expected payday passes with no payslip
→ current cycle remains open

bonus income
→ INCOME
→ no cycle reset

THR
→ INCOME
→ no cycle reset

non-anchor spouse salary
→ INCOME
→ no cycle reset

missing pay date
→ review asks user
→ no guessed salary boundary

duplicate payslip for same salary source + payroll period
→ no duplicate canonical income
```

---

# 17. Queue Architecture Acceptance Criteria

The queue architecture is acceptable only if:

```text
interactive jobs have reserved execution capacity
background LLM/vision jobs cannot starve buttons
callback ACK is independent from processing completion
button actions do not invoke LLM
retries remain idempotent
financial state mutation is auditable
notification/edit failure cannot roll back committed financial state
```

Add observability for:

```text
callback webhook latency
callback ACK latency
interactive queue depth
interactive job age
background queue depth
Telegram edit/send failure rate
```

---

# 18. Revised Codex Implementation Order

Follow the mandatory worktree workflow in `AGENTS.md`.

Recommended order:

```text
1. Telegram callback fast ACK path
2. PROCESS_TELEGRAM_CALLBACK deterministic handler
3. interactive/default/background worker lanes
4. editMessageText + editMessageReplyMarkup
5. callback idempotency + latency tests

6. document_page schema
7. Telegram media_group_id intake
8. album finalization/debounce job
9. web multi-image upload
10. multi-page classifier/extractor calls

11. universal review_item
12. deterministic review recipient resolver

13. salary_source
14. salary_event
15. payslip salary confirmation flow
16. dynamic financialperiod resolver

17. cycle-aware Ringkasan
18. cycle-aware Analisis
19. cycle-aware Telegram queries
20. cycle-aware Insights

21. Indonesian copy sweep
22. chart/UI polish
23. remaining correctness/operations hardening
24. full regression and production verification
```

Do not block callback architecture work on salary-cycle or image implementation.

---

# 19. Suggested Branch Decomposition

Examples:

```text
perf/telegram-interactive-callbacks
feat/telegram-message-editing
feat/telegram-worker-lanes
feat/multi-image-documents
feat/universal-review-items
feat/salary-cycle-domain
feat/payslip-salary-cycle
feat/cycle-aware-analytics
feat/cycle-aware-telegram
feat/product-copy-polish
feat/salary-cycle-ui
```

Each user-requested repository change must still follow `AGENTS.md` worktree rules.

---

# 20. Explicitly Deferred

Do not implement in this iteration:

```text
PDF payslip support
salary bank ingestion
salary bank Gmail parser
salary-credit detection
payslip-to-bank-credit reconciliation
portfolio/assets
budget-first UX
bank/payment execution
```

---

# 21. Final Target Behavior

## Telegram interaction

```text
User taps [Konfirmasi]
        ↓
spinner stops almost immediately
        ↓
financial action processed deterministically
        ↓
same Telegram message updates
        ↓
buttons disappear
```

This must remain responsive even while document vision or insight generation is running.

## Payslip

```text
User sends 3 payslip screenshots as one Telegram album
        ↓
3 immutable source events
        ↓
ONE logical payslip document
        ↓
ONE extraction
        ↓
correct member receives salary review
        ↓
confirmed pay date becomes salary-cycle boundary
```

## Finance period

```text
25 Aug salary
24 Sep salary

Cycle A = 25 Aug – 23 Sep
Cycle B = 24 Sep – next confirmed salary
```

Richmod should feel like a professional financial assistant that understands the household's real cashflow rhythm and responds immediately to interactive actions.
