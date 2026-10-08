# RICHMOD WEALTH, SAVINGS & CYCLE RECONCILIATION V1
## Final Full PRD for Coding Agent

**Status:** FINAL / FROZEN IMPLEMENTATION CONTRACT
**Repository:** `raufimusaddiq/richmod`
**Baseline branch:** `main`
**Pinned baseline commit:** `e405bc72ce6d3f2e70a35de0e63341d91b46dd01`
**Baseline date:** 2026-09-06
**Highest migration at baseline:** `00045_one_active_household_per_user.sql`
**Next migration at baseline:** `00046_wealth_savings_cycle_reconciliation.sql`
**Backend:** Go
**Frontend:** Next.js + React + JavaScript
**Database:** PostgreSQL
**LLM:** Cloud LLM Gateway only
**Money:** PostgreSQL `NUMERIC`, API decimal strings, never float
**Currency:** IDR only
**Timezone:** Asia/Jakarta

---

# 0. Normative Instructions

This document is the latest explicit product instruction for Richmod Wealth/Savings and overrides older wealth drafts.

The coding agent MUST first:

1. fetch current `main`;
2. verify whether this pinned commit is still current or an ancestor;
3. inspect `db/migrations`;
4. use the next unused migration number if `00046` is no longer available;
5. read `AGENTS.md`, current relevant ADRs, current salary/review/ledger/Telegram implementation;
6. create a dedicated branch and linked worktree according to repository rules;
7. preserve all unrelated current behavior.

Suggested branch:

```text
feat/wealth-savings-cycle-reconciliation
```

This is an additive product expansion.

**Do not rewrite the stable ledger, salary, evidence, review, email-ingress, or Telegram architecture unless a narrowly scoped change is required by this PRD.**

---

# 1. Primary Product Goal

Richmod must evolve from household income/expense tracking into a household financial system that answers, canonically:

```text
What did we earn?
What did we spend?
How much did we intentionally save?
Where did the saving go?
What remained from the salary cycle?
Where did that residual end up?
What assets/liabilities do we currently own?
What are they worth now?
What is current Net Worth?
Why did Net Worth change?
```

The core flow is:

```text
PRIMARY SALARY
      ↓
SAVE / INVEST
      ↓
SPEND
      ↓
CYCLE RECAP
      ↓
RECONCILE RESIDUAL
      ↓
OBSERVE CURRENT WEALTH
      ↓
NET WORTH
```

---

# 2. Non-Negotiable Backward Compatibility

The following existing flows are already stable and MUST remain stable:

```text
manual INCOME / EXPENSE creation
transaction status lifecycle
transaction evidence
reconciliation / reversible merges
Bank Email ingestion
Cloudflare email ingress
generic document pipeline
payslip extraction + PAYSLIP_CONFIRMATION
PRIMARY_SALARY vs ORDINARY_INCOME
salary_source / salary_event
salary_cycle_bounds(...)
transfer classification review
known_account matching
universal Review Inbox
Telegram durable ingress
Telegram native-only finance tool contract
Telegram bound review replies
merchant learning opt-in
analytics period semantics
one active household per user
```

Feature implementation MUST be additive around these behaviors.

No migration may rewrite existing transaction amounts, dates, statuses, evidence, category, merchant, or account relationships except the explicit conservative `purpose` backfill defined below.

---

# 3. Existing Salary Contract to Retain

Current canonical salary data:

```text
salary_source
salary_event
```

A confirmed primary payslip produces:

```text
transaction.type   = INCOME
transaction.status = CONFIRMED

salary_event
  pay_date
  payroll_period
  net_pay
  transaction_id
  status = CONFIRMED
```

The current salary-cycle resolver uses:

```text
salary_cycle_bounds(household, as_of)
```

with:

```text
cycle_start = latest confirmed primary salary pay_date <= as_of
cycle_end   = next confirmed primary salary pay_date > as_of
```

The cycle is half-open:

```text
[cycle_start, cycle_end)
```

If there is no next primary salary event, the current cycle remains open.

**This feature MUST reuse this contract.**

Do not introduce a competing salary-cycle engine.

---

# 4. Core Financial Model

Richmod shall have three distinct financial layers:

```text
CASHFLOW
    ↓
SAVINGS ALLOCATION
    ↓
WEALTH
```

## 4.1 Cashflow

Canonical source:

```text
transaction
```

Answers:

```text
income
expense
refund
cashflow surplus
```

## 4.2 Savings Allocation

Canonical source:

```text
confirmed TRANSFER + purpose
```

Answers:

```text
how much was intentionally saved/invested
where it was allocated
```

Savings Allocation is not a balance and is not a second ledger.

## 4.3 Wealth

Canonical source:

```text
wealth_account
wealth_snapshot
wealth_snapshot_item
```

Answers:

```text
current observed asset value
current observed liability balance
Net Worth
Net Worth history
```

Wealth is point-in-time.

It is not salary-cycle state.

---

# 5. Ledger Invariants

These rules are mandatory.

## 5.1 Saving is not expense

The following MUST NOT count as expense:

```text
own-account transfer
household-account transfer
transfer to savings
RDN/brokerage contribution
mutual-fund contribution
gold asset purchase
loan principal repayment
```

## 5.2 Asset appreciation is not saving

Example:

```text
previous mutual fund value  Rp39m
new contribution             Rp3m
new observed value          Rp42.7m
```

Then:

```text
Savings Allocated            Rp3m
Valuation / Other             Rp0.7m
```

## 5.3 Asset sale/withdrawal is not income

Money returned from:

```text
RDN
mutual fund
gold sale
savings account
```

is not automatically household `INCOME`.

V1 records a known internal return of cash as:

```text
TRANSFER
INTERNAL_TRANSFER
```

unless another existing canonical transaction semantic applies.

## 5.4 No synthetic Savings account

Do not create a virtual asset named:

```text
Savings
```

from:

```text
Income - Expense
```

## 5.5 Snapshot is not transaction

Creating a wealth snapshot MUST NOT create:

```text
INCOME
EXPENSE
TRANSFER
REFUND
ADJUSTMENT
```

## 5.6 Residual attribution is not transaction

Cycle residual review is explanatory/reconciliation metadata.

Allocating cycle residual to Jago/Blu/Cash/etc. MUST NOT create a transaction unless the user separately records an actual missing transaction.

---

# 6. Transaction Purpose

Add:

```sql
transaction.purpose TEXT
```

Allowed values:

```text
GENERAL
INTERNAL_TRANSFER
SAVINGS_TRANSFER
INVESTMENT_CONTRIBUTION
ASSET_PURCHASE
DEBT_PRINCIPAL_PAYMENT
```

## 6.1 Conservative backfill

Existing rows:

```text
TRANSFER    -> INTERNAL_TRANSFER
everything else -> GENERAL
```

Do not infer historical savings from description/counterparty during migration.

After backfill:

```text
purpose NOT NULL
```

## 6.2 Allowed combinations

| transaction.type | purpose |
|---|---|
| UNCLASSIFIED | GENERAL |
| INCOME | GENERAL |
| EXPENSE | GENERAL |
| REFUND | GENERAL |
| ADJUSTMENT | GENERAL |
| TRANSFER | INTERNAL_TRANSFER |
| TRANSFER | SAVINGS_TRANSFER |
| TRANSFER | INVESTMENT_CONTRIBUTION |
| TRANSFER | ASSET_PURCHASE |
| TRANSFER | DEBT_PRINCIPAL_PAYMENT |

Enforce in Go and DB CHECK.

---

# 7. Savings Allocated

A transaction counts as savings exactly when:

```text
status = CONFIRMED
AND type = TRANSFER
AND purpose IN (
  SAVINGS_TRANSFER,
  INVESTMENT_CONTRIBUTION,
  ASSET_PURCHASE
)
```

Formula:

```text
SavingsAllocated =
SUM(amount)
```

for those rows.

There is no `counts_as_savings` column.

---

# 8. Canonical Transaction Examples

## 8.1 Top up RDN

```text
"3 juta top up RDN"
```

Canonical:

```text
type = TRANSFER
purpose = INVESTMENT_CONTRIBUTION
amount = 3,000,000
related_wealth_account_id = RDN
```

## 8.2 Buy gold

```text
"1 juta beli emas"
```

Canonical:

```text
type = TRANSFER
purpose = ASSET_PURCHASE
related_wealth_account_id = Gold
```

## 8.3 Save into Blu

```text
"2 juta pindah ke Blu buat tabungan"
```

Canonical:

```text
type = TRANSFER
purpose = SAVINGS_TRANSFER
related_wealth_account_id = Blu
```

## 8.4 Ordinary own-account movement

```text
"2 juta Jago ke BCA"
```

without explicit savings intent:

```text
type = TRANSFER
purpose = INTERNAL_TRANSFER
```

## 8.5 Debt principal

```text
type = TRANSFER
purpose = DEBT_PRINCIPAL_PAYMENT
related_wealth_account_id = liability
```

Does not count as expense or savings.

---

# 9. Wealth Account

Create:

```sql
wealth_account
```

Exact fields:

```text
id UUID PK
household_id UUID NOT NULL
name TEXT NOT NULL
institution TEXT NULL

side:
  ASSET
  LIABILITY

wealth_type:
  BANK
  CASH
  EWALLET
  MUTUAL_FUND
  GOLD
  BROKERAGE
  DEPOSIT
  CRYPTO
  LOAN
  OTHER

usage_role:
  TRANSACTIONAL
  SAVINGS
  INVESTMENT
  OTHER

owner_user_id UUID NULL
linked_account_id UUID NULL

active BOOLEAN NOT NULL DEFAULT true

created_at TIMESTAMPTZ
updated_at TIMESTAMPTZ
```

Unique:

```text
(household_id, name)
```

## 9.1 Ownership

```text
owner_user_id = NULL
```

means joint/household.

Non-null owner must be an active member of same household.

## 9.2 Linked transaction account

Optional:

```text
linked_account_id -> account.id
```

Same household only.

One active Wealth Account may link to a transaction Account.

Partial unique index required.

## 9.3 Valid side/type

ASSET:

```text
BANK
CASH
EWALLET
MUTUAL_FUND
GOLD
BROKERAGE
DEPOSIT
CRYPTO
OTHER
```

LIABILITY:

```text
LOAN
OTHER
```

LIABILITY requires:

```text
usage_role = OTHER
```

No hard delete.

---

# 10. Transaction to Wealth Link

Add:

```sql
transaction.related_wealth_account_id UUID NULL
```

Required when purpose is:

```text
SAVINGS_TRANSFER
INVESTMENT_CONTRIBUTION
ASSET_PURCHASE
DEBT_PRINCIPAL_PAYMENT
```

Must be same household.

For V1:

```text
GENERAL
INTERNAL_TRANSFER
```

must keep this null.

---

# 11. Known Account to Wealth Link

Add:

```sql
known_account.wealth_account_id UUID NULL
```

Same household only.

This is used to make existing deterministic transfer classification more useful.

Example:

```text
known_account.relationship = INVESTMENT_ACCOUNT
known_account.wealth_account_id = RDN
```

Then a deterministic investment-transfer classification can set:

```text
TRANSFER
INVESTMENT_CONTRIBUTION
RDN
```

Do not mutate historical transactions when the link is configured.

---

# 12. Wealth Snapshot

Current value is entered primarily through web.

Create:

```sql
wealth_snapshot
```

Fields:

```text
id UUID PK
household_id UUID NOT NULL
observed_at TIMESTAMPTZ NOT NULL
created_by_user_id UUID NULL
created_at TIMESTAMPTZ
updated_at TIMESTAMPTZ
```

Unique:

```text
(household_id, observed_at)
```

Create:

```sql
wealth_snapshot_item
```

Fields:

```text
id UUID PK
snapshot_id UUID NOT NULL
wealth_account_id UUID NOT NULL
value_idr NUMERIC(20,0) NOT NULL CHECK >= 0

quantity NUMERIC(30,10) NULL CHECK >= 0
unit TEXT NULL
unit_price_idr NUMERIC(20,0) NULL CHECK >= 0

source:
  MANUAL
  DOCUMENT
  SYSTEM

note TEXT NULL
created_at
updated_at
```

Unique:

```text
(snapshot_id, wealth_account_id)
```

---

# 13. Full Snapshot Rule

Every V1 snapshot is a complete household wealth observation.

For a new snapshot:

```text
exactly one item for every active wealth_account
```

Backend rejects:

```text
missing active account
duplicate account
cross-household account
inactive account in new snapshot
```

Frontend may visibly prefill previous values.

Saving means the user explicitly confirms all displayed values for `observed_at`.

Backend MUST NOT silently carry stale values forward.

---

# 14. Current Value Behavior

## Bank/Cash/E-wallet

Observed balance is manually confirmed in snapshot.

Even if transactions exist, snapshot is authoritative for Wealth because not every account is guaranteed to be full-ledger tracked.

## Mutual Fund

Contribution:

```text
transaction
```

Current value:

```text
snapshot
```

## RDN/Brokerage

Contribution:

```text
transaction
```

Current total portfolio/account value:

```text
snapshot
```

## Gold

Current value:

```text
snapshot.value_idr
```

Optional:

```text
quantity
unit
unit_price_idr
```

No automatic market-price API in V1.

---

# 15. Net Worth

Per complete snapshot:

```text
TotalAssets =
SUM(value_idr where side=ASSET)

TotalLiabilities =
SUM(value_idr where side=LIABILITY)

NetWorth =
TotalAssets - TotalLiabilities
```

Historical snapshot items remain valid after account deactivation.

---

# 16. Net Worth Change and Valuation Reconciliation

For previous snapshot S1 and current S2:

```text
NetWorthChange =
NetWorth(S2) - NetWorth(S1)
```

Cashflow interval:

```text
transaction_at > S1.observed_at
AND transaction_at <= S2.observed_at
```

Then:

```text
ValuationOtherMovement =
NetWorthChange
- CashflowSurplusBetweenSnapshots
```

UI label exactly:

```text
Valuation / Other Wealth Movement
```

Never label V1 residual as:

```text
Investment Return
Profit
P&L
```

---

# 17. Salary Cycle Analytics

Current period selection remains existing behavior.

When primary salary cycle exists:

```text
[pay_date, next_primary_pay_date)
```

Otherwise existing calendar fallback remains.

## Existing metrics retain current meaning

```text
income
expense
netCashflow
savingsRate
```

Do not redefine them.

## New metrics

```text
savingsAllocated
savingsAllocationRate
rawUnallocatedSurplus
```

Formula:

```text
rawUnallocatedSurplus =
Income
- Expense
- SavingsAllocated
```

May be negative.

Do not clamp in backend.

---

# 18. Cycle Allocation by Destination

For a salary cycle:

```text
Savings Allocation by Destination
```

is:

```text
confirmed savings-purpose TRANSFER
GROUP BY related_wealth_account_id
```

This answers:

```text
RDN  Rp3m
Gold Rp1m
Blu  Rp2m
```

Only canonical confirmed transactions participate.

---

# 19. New Requirement: Cycle Residual Reconciliation

When a new confirmed PRIMARY salary event creates the next cycle boundary, the previous cycle becomes a closed interval.

Example:

```text
previous cycle:
25 Aug <= t < 25 Sep

new primary salary:
25 Sep
```

Calculate:

```text
Income
Expense
SavingsAllocated
RawResidual =
Income - Expense - SavingsAllocated
```

If:

```text
RawResidual > 0
```

Richmod must create one non-blocking:

```text
CYCLE_RESIDUAL_ALLOCATION
```

review for the previous cycle.

This review answers:

> The previous salary cycle still has income that was neither recorded as expense nor intentional saving. Where did it end up?

---

# 20. New Table: Cycle Residual Case

Create:

```sql
cycle_residual_case
```

Fields:

```text
id UUID PK

household_id UUID NOT NULL

start_salary_event_id UUID NOT NULL
end_salary_event_id UUID NOT NULL

cycle_start DATE NOT NULL
cycle_end DATE NOT NULL   -- exclusive

basis_income_idr NUMERIC(20,0) NOT NULL
basis_expense_idr NUMERIC(20,0) NOT NULL
basis_savings_idr NUMERIC(20,0) NOT NULL
basis_residual_idr NUMERIC(20,0) NOT NULL CHECK > 0

created_at TIMESTAMPTZ
updated_at TIMESTAMPTZ
```

Unique:

```text
(household_id, start_salary_event_id, end_salary_event_id)
```

The row is not a ledger.

It captures the deterministic residual basis used when the review was created.

---

# 21. New Table: Cycle Residual Allocation

Create:

```sql
cycle_residual_allocation
```

Fields:

```text
id UUID PK
cycle_residual_case_id UUID NOT NULL
wealth_account_id UUID NOT NULL

amount_idr NUMERIC(20,0) NOT NULL CHECK > 0

note TEXT NULL
created_by_user_id UUID NOT NULL

created_at TIMESTAMPTZ
updated_at TIMESTAMPTZ
```

Meaning:

```text
user-attributed location of residual money
```

It is NOT:

```text
a transaction
saving contribution
expense
wealth valuation
```

Same household validation mandatory.

---

# 22. Residual Review Integration with Universal Review

Extend `review_item.review_type` with:

```text
CYCLE_RESIDUAL_ALLOCATION
```

Extend `review_item` with:

```text
cycle_residual_case_id UUID NULL
```

Update the universal subject CHECK so a review may be bound to exactly one supported subject, including cycle residual case.

Add active uniqueness:

```text
one OPEN/PENDING_SEND review per cycle_residual_case
```

Do not create a parallel review system.

Reuse:

```text
review_item
review_request
household-wide review routing
Telegram bound review delivery
Inbox
resolution audit
```

---

# 23. Residual Review Trigger Safety

**Primary salary confirmation must never be rolled back because residual-review generation failed.**

This is a hard safety requirement.

Therefore:

1. preserve current payslip/PRIMARY_SALARY canonical transaction + salary_event transaction;
2. after successful salary commit, schedule/materialize residual review in a separate idempotent path;
3. failure to create the residual review must not undo salary income;
4. provide a catch-up mechanism that can regenerate missing residual reviews idempotently.

Implementation may use the existing PostgreSQL job queue.

Recommended job:

```text
GENERATE_CYCLE_RESIDUAL_REVIEW
```

payload:

```text
household_id
end_salary_event_id
```

The job must be idempotent.

Additionally, worker startup/periodic reconciliation MAY scan recent closed primary salary cycles without cases so a lost enqueue cannot permanently lose a review.

Do not modify salary-cycle semantics.

---

# 24. Residual Review Actions

Canonical actions:

```text
ALLOCATE_RETAINED_BALANCE
TRANSACTION_MISSING
LEAVE_UNALLOCATED
```

## 24.1 ALLOCATE_RETAINED_BALANCE

User provides one or more:

```text
wealthAccountId
amountIdr
```

Example:

```text
Jago Rp700k
Cash Rp300k
```

The sum MUST equal the currently valid positive residual basis.

This writes only:

```text
cycle_residual_allocation
```

It does not write a transaction.

Then resolve the review.

## 24.2 TRANSACTION_MISSING

This does not directly mutate the ledger from residual metadata.

Instead route the user into the existing canonical transaction flow:

```text
record missing expense
or
record/reconcile missing transfer
```

After canonical transaction is created/reconciled, recompute residual.

If residual remains positive, keep/recreate the residual review.

If residual becomes zero/non-positive, resolve/cancel the residual review as no longer applicable.

This keeps ledger mutation centralized.

## 24.3 LEAVE_UNALLOCATED

Resolve review with no allocation.

Historical recap shows:

```text
Unallocated
```

No fake transaction.

---

# 25. Residual Review Staleness

Historical transactions may later be corrected or added.

Therefore a cycle residual case must not silently remain authoritative if its basis no longer matches the ledger.

Before resolving a residual review:

1. recompute:
   ```text
   income
   expense
   savingsAllocated
   rawResidual
   ```
   for exact stored cycle bounds;
2. compare with case basis;
3. if different:
   - do not accept old allocation;
   - update/recreate case basis safely;
   - return/emit a refresh message;
   - keep review open if new residual > 0;
   - resolve/cancel if new residual <= 0.

Cycle Recap API must expose whether stored residual attribution is:

```text
CURRENT
STALE
NOT_REVIEWED
LEFT_UNALLOCATED
RESOLVED
```

Do not silently apply stale attribution.

---

# 26. Cycle Residual Is Not Carry-Over Income

When next primary salary arrives:

Previous cycle residual:

```text
does not become new INCOME
does not become new SavingsAllocated
does not become a synthetic transaction
does not get copied into new-cycle raw residual
```

Physical money remains part of real account balances and later Wealth snapshots.

New cycle analytics begin from new cycle transactions only.

---

# 27. Transaction Date Controls Cycle Attribution

If an actual saving transfer happened:

```text
24 Sep
```

it belongs to the old cycle if old cycle is:

```text
[25 Aug, 25 Sep)
```

If it happened:

```text
26 Sep
```

it belongs to the new cycle even if user says:

```text
"this was leftover salary from last month"
```

Cycle attribution follows:

```text
transaction_at
```

not mental source-of-funds interpretation.

---

# 28. Negative Raw Residual

Possible:

```text
SavingsAllocated + Expense > Income
```

because the household may use prior cash.

Backend keeps:

```text
rawUnallocatedSurplus < 0
```

Do not create Cycle Residual Review.

UI label for negative value:

```text
Over-allocated vs cycle income
```

Do not display it as a negative “remaining salary”.

---

# 29. Cycle Recap Formula

Before residual attribution:

```text
Income =
Expense
+ SavingsAllocated
+ RawResidual
```

After retained-balance review:

```text
PositiveRawResidual =
RetainedAttributed
+ RemainingUnresolvedResidual
```

The recap must never double-count retained attribution as SavingsAllocated.

---

# 30. Cycle Recap Example

Cycle:

```text
Income                  Rp16m
Expense                  Rp9m
Savings Allocated        Rp6m
Raw Residual             Rp1m
```

Saving destinations:

```text
RDN                      Rp3m
Blu                      Rp2m
Gold                     Rp1m
```

Residual review:

```text
Jago                     Rp1m
```

Final explanatory recap:

```text
Where cycle income went

Expense                  Rp9m
RDN                      Rp3m
Blu                      Rp2m
Gold                     Rp1m
Retained in Jago         Rp1m
------------------------------
Total                    Rp16m
```

`Retained in Jago` does not create transaction or savings.

---

# 31. Cycle Recap API

Add:

```text
GET /api/v1/wealth/cycle-recaps
```

Query:

```text
?limit=12
```

Return closed salary cycles derived from confirmed primary salary events.

Example item:

```json
{
  "cycleStart": "2026-08-25",
  "cycleEnd": "2026-09-25",
  "income": "16000000",
  "expense": "9000000",
  "cashflowSurplus": "7000000",
  "savingsAllocated": "6000000",
  "rawResidual": "1000000",
  "savingsByDestination": [
    {
      "wealthAccountId": "...",
      "name": "RDN",
      "amount": "3000000"
    }
  ],
  "residualAttributions": [
    {
      "wealthAccountId": "...",
      "name": "Jago",
      "amount": "1000000"
    }
  ],
  "residualReviewStatus": "RESOLVED"
}
```

No cycle row is required for the current open cycle.

---

# 32. Wealth REST API

## Wealth Accounts

```text
GET   /api/v1/wealth/accounts
POST  /api/v1/wealth/accounts
PATCH /api/v1/wealth/accounts/{id}
```

OWNER only for create/update/deactivate.

Any active member can read.

No DELETE.

## Snapshots

```text
GET  /api/v1/wealth/snapshots
GET  /api/v1/wealth/snapshots/latest
GET  /api/v1/wealth/snapshots/{id}
POST /api/v1/wealth/snapshots
PUT  /api/v1/wealth/snapshots/{id}
```

Any active member can create/correct snapshot.

No DELETE.

## Summary

```text
GET /api/v1/wealth/summary
```

## History

```text
GET /api/v1/wealth/history
```

## Cycle recap

```text
GET /api/v1/wealth/cycle-recaps
```

---

# 33. Snapshot API Contract

POST body:

```json
{
  "observedAt": "2026-09-30T21:00:00+07:00",
  "items": [
    {
      "wealthAccountId": "...",
      "valueIdr": "42700000",
      "quantity": null,
      "unit": null,
      "unitPriceIdr": null,
      "source": "MANUAL",
      "note": null
    }
  ]
}
```

Money uses string.

POST same `(household, observedAt)`:

```text
409
```

PUT corrects a specific snapshot atomically.

`observedAt` immutable.

---

# 34. Manual Transaction API

Current manual endpoint is stable for INCOME/EXPENSE.

Do not break existing payloads.

Extend backward-compatibly to allow:

```text
TRANSFER
```

with:

```text
purpose
relatedWealthAccountId
```

Only for explicit manual transfer creation.

Existing INCOME/EXPENSE clients need not send `purpose`.

Server assigns:

```text
GENERAL
```

for them.

Do not duplicate the current source_event/evidence/audit path.

Refactor into a reusable ledger service only if required to avoid duplicated mutation logic.

---

# 35. Transfer Review Mapping

Existing actions retain behavior:

```text
OWN_ACCOUNT_TRANSFER
HOUSEHOLD_TRANSFER
```

map to:

```text
TRANSFER
INTERNAL_TRANSFER
```

Existing:

```text
INVESTMENT_TRANSFER
```

maps to:

```text
TRANSFER
INVESTMENT_CONTRIBUTION
```

only when destination Wealth Account resolves deterministically.

If destination cannot resolve:

- remain non-expense according to current transfer semantics;
- do not count as SavingsAllocated;
- keep/request review/configuration.

Do not fabricate Wealth Account automatically.

---

# 36. Web Navigation

Add:

```text
Kekayaan
/wealth
```

Current target navigation:

```text
Ringkasan
Transaksi
Analisis
Kekayaan
Inbox
Dokumen
Keluarga
Pengaturan
```

Preserve current mobile navigation behavior.

---

# 37. Wealth Page

`/wealth` sections:

1. Net Worth summary
2. Total Assets
3. Total Liabilities
4. Asset list grouped by usage role
5. Liability list
6. Net Worth History
7. Latest snapshot reconciliation
8. Current Cycle Savings
9. Closed Cycle Recaps
10. Snapshot History
11. Update Current Values

---

# 38. Web Current-Value Input

Primary V1 current-value input is web.

Flow:

```text
/wealth
  ↓
Update Nilai
  ↓
load all active Wealth Accounts
  ↓
show previous value
  ↓
prefill previous value visibly
  ↓
user verifies/edits all
  ↓
Save Snapshot
```

The user may create a snapshot at any time.

It is not restricted to salary-cycle end.

---

# 39. Cycle Recap UI

For a closed salary cycle:

```text
25 Agu – 24 Sep
```

Show:

```text
Pemasukan
Pengeluaran
Ditabung
Sisa Cycle
```

Then:

```text
Alokasi Tabungan

RDN
Gold
Blu
```

Then:

```text
Rekonsiliasi Sisa

Jago
Cash
Unallocated
```

If residual review is open:

```text
Sisa Rp1.000.000 belum direkonsiliasi.
[Tinjau]
```

---

# 40. Residual Review UI

Inbox card:

```text
Sisa salary cycle perlu direkonsiliasi

25 Agu – 24 Sep
Sisa belum teralokasi: Rp1.000.000

Ke mana sisa ini akhirnya berada?
```

Actions:

```text
[Alokasikan saldo tersisa]
[Ada transaksi yang belum tercatat]
[Biarkan belum teralokasi]
```

Allocation editor supports multiple Wealth Accounts.

Must show:

```text
This is attribution only and does not create a transaction.
```

If user chooses transaction missing:

route to standard canonical transaction entry/review path.

---

# 41. Overview UI

Current-period metrics:

```text
Pemasukan
Pengeluaran
Ditabung
Sisa Belum Dialokasikan
```

Mapping:

```text
income
expense
savingsAllocated
rawUnallocatedSurplus
```

Negative raw residual label:

```text
Over-allocated vs cycle income
```

Compact card:

```text
Kekayaan Bersih
latest Net Worth
observed timestamp
change vs previous snapshot
```

No snapshot:

```text
Kekayaan belum diatur
```

Never fake `Rp0`.

---

# 42. Analytics

Preserve existing analytics.

Add:

```text
Savings Allocated by Cycle
Savings Allocation Rate
Savings by Destination
Cycle Residual
Net Worth History
```

Existing `savingsRate` keeps its current cashflow-surplus meaning.

New:

```text
savingsAllocationRate =
SavingsAllocated / Income
```

---

# 43. Telegram V1

Extend existing native-only tool registry.

Do not build another bot.

Support:

```text
record_transfer
query_savings
query_wealth
list_wealth_accounts
resolve cycle residual review
```

V1 does not create partial Wealth Snapshots from Telegram.

Current values remain web-primary.

---

# 44. Telegram Transfer Safety

User:

```text
3 juta top up RDN
```

Flow:

1. inspect existing evidence/transaction candidate;
2. resolve source account;
3. resolve destination Wealth Account;
4. deduplicate/reconcile;
5. if unique:
   ```text
   TRANSFER
   INVESTMENT_CONTRIBUTION
   ```
6. if source/destination ambiguous:
   clarify/review.

Never silently assume Jago.

---

# 45. Telegram Residual Review

Because universal review is household-wide, residual review should work through existing Telegram review routing.

Example:

```text
Cycle 25 Agu – 24 Sep masih punya sisa Rp1.000.000.

Ke mana sisa ini akhirnya berada?
```

Model may interpret:

```text
"700 ribu masih di Jago, 300 ribu cash"
```

but Go must:

- resolve Wealth Accounts deterministically;
- verify amounts sum to current valid residual;
- validate same household;
- persist only `cycle_residual_allocation`;
- resolve bound review.

No model-direct mutation.

---

# 46. Evidence and Dedup

Do not create a Wealth-specific transaction dedup engine.

Reuse current:

```text
source_event
transaction_evidence
transaction_proposal
reconciliation
review
```

Financial Email now enters through Cloudflare runtime.

Do not reintroduce Gmail OAuth/watch.

---

# 47. Review vs Integration Action

Use:

```text
Review
```

for:

```text
financial ambiguity
cycle residual reconciliation
missing transaction
transfer meaning
```

Use:

```text
Integration Action
```

only for operational integration/setup issues.

Do not route residual review to Integration Actions.

---

# 48. Permissions

Any active household member:

```text
read Wealth Accounts
read Wealth
create/correct full snapshot
read cycle recap
resolve cycle residual allocation
query through Telegram
```

OWNER only:

```text
create/update/deactivate Wealth Account
link transaction Account
link Known Account
change Wealth Account owner
```

All same-household.

---

# 49. Audit

Reuse current `audit_log`.

Required events include:

```text
WEALTH_ACCOUNT_CREATE
WEALTH_ACCOUNT_UPDATE
WEALTH_SNAPSHOT_CREATE
WEALTH_SNAPSHOT_UPDATE

TRANSACTION_PURPOSE_UPDATE
TRANSACTION_WEALTH_LINK_UPDATE
KNOWN_ACCOUNT_WEALTH_LINK_UPDATE

CYCLE_RESIDUAL_CASE_CREATE
CYCLE_RESIDUAL_ALLOCATE
CYCLE_RESIDUAL_LEAVE_UNALLOCATED
CYCLE_RESIDUAL_STALE
```

Follow current repo action naming style consistently.

No parallel audit table.

---

# 50. Migration `00046`

At pinned baseline create:

```text
00046_wealth_savings_cycle_reconciliation.sql
```

It must:

1. add `transaction.purpose`;
2. backfill conservatively;
3. add NOT NULL + checks;
4. create `wealth_account`;
5. add `transaction.related_wealth_account_id`;
6. add `known_account.wealth_account_id`;
7. create `wealth_snapshot`;
8. create `wealth_snapshot_item`;
9. create `cycle_residual_case`;
10. create `cycle_residual_allocation`;
11. add `review_item.cycle_residual_case_id`;
12. extend review type CHECK;
13. update review subject CHECK;
14. add active unique review index for cycle residual;
15. add relevant indexes.

No existing table is dropped.

---

# 51. Suggested Indexes

```text
wealth_account(household_id, active, usage_role, name)

wealth_snapshot(household_id, observed_at DESC)

wealth_snapshot_item(wealth_account_id, snapshot_id)

cycle_residual_case(household_id, cycle_end DESC)

cycle_residual_allocation(cycle_residual_case_id)

review_item(cycle_residual_case_id)
  WHERE cycle_residual_case_id IS NOT NULL
```

---

# 52. Go Architecture

Create:

```text
apps/api/internal/wealth
```

Responsibilities:

```text
Wealth Account validation
Wealth Account CRUD
snapshot validation/persistence
Net Worth
history
snapshot reconciliation
cycle recap queries
cycle residual case computation
cycle residual allocations
```

Do not move existing ledger business rules into Wealth.

Ledger remains canonical transaction mutation owner.

Salary remains canonical salary-source/event owner.

Review remains canonical review owner.

---

# 53. Required Service Boundaries

Avoid SQL duplication of money rules across handlers.

Create reusable deterministic functions/services for:

```text
CalculatePeriodCashflow(...)
CalculateSavingsAllocated(...)
CalculateSavingsByDestination(...)
ResolveSalaryCycle(...)
CalculateCycleResidual(...)
```

Existing analytics logic should be refactored carefully into shared helpers only if tests prove unchanged output.

Do not rewrite existing analytics wholesale.

---

# 54. Salary Hook

When a new `PRIMARY_SALARY` is successfully confirmed:

```text
existing income transaction
existing salary_source semantics
existing salary_event
```

must commit exactly as today.

Only afterward:

```text
enqueue GENERATE_CYCLE_RESIDUAL_REVIEW
```

for previous closed cycle.

If enqueue fails:

```text
salary confirmation remains successful
```

A catch-up path must repair missing generation.

---

# 55. Idempotency

Mandatory unique/idempotency keys:

## Salary residual case

```text
(household_id, start_salary_event_id, end_salary_event_id)
```

## Active residual review

one active review per:

```text
cycle_residual_case_id
```

## Snapshot

```text
(household_id, observed_at)
```

## Existing source event / evidence

retain current keys.

Retrying a job must never create duplicate:

```text
cycle case
review
transaction
snapshot
allocation
```

---

# 56. Concurrency

Use transaction + row locking for:

```text
cycle residual resolution
snapshot correction
Wealth Account link changes
transaction purpose enrichment
```

Residual allocation resolution:

1. lock residual case;
2. recompute ledger basis;
3. verify still current;
4. validate allocation sum;
5. delete/replace prior allocation rows only inside same transaction if retry/edit;
6. resolve review;
7. audit;
8. commit.

Do not allow two household members to double-resolve.

Second resolver receives:

```text
409 already resolved / changed
```

---

# 57. Ledger Mutation Safety

Wealth code MUST NOT directly insert/update financial transactions except through shared ledger/review services.

Specifically:

```text
cycle residual allocation
```

must never do:

```sql
INSERT INTO transaction ...
```

The “missing transaction” branch must route to standard transaction creation/reconciliation.

This is the strongest guard against ledger regression.

---

# 58. Transaction Status Safety

Only:

```text
CONFIRMED
```

transactions count for:

```text
Income
Expense
SavingsAllocated
cycle recap
snapshot cashflow reconciliation
```

Do not count:

```text
PENDING
NEEDS_REVIEW
VOIDED
```

Existing lifecycle remains unchanged.

---

# 59. Refund Safety

Preserve current:

```text
Expense =
confirmed EXPENSE
- confirmed REFUND
```

Do not reinterpret REFUND as savings.

---

# 60. Transfer Safety

`TRANSFER` is neutral to expense analytics.

Purpose only adds semantic intent.

No purpose may cause transfer amount to also become EXPENSE.

One financial event must not be represented as:

```text
TRANSFER + EXPENSE
```

unless there are genuinely two separate canonical transactions/evidence-backed components.

---

# 61. Wealth Reconciliation Does Not Mutate Ledger

If:

```text
snapshot observed bank balance
!=
transaction-derived expectation
```

display discrepancy.

Do not auto-create:

```text
ADJUSTMENT
INCOME
EXPENSE
```

Any adjustment requires explicit canonical user action/review.

---

# 62. First-Time Wealth Setup

Flow:

```text
No Wealth Accounts
  ↓
OWNER creates Wealth Accounts
  ↓
optional transaction-account links
  ↓
optional known-account links
  ↓
household member creates full opening snapshot
  ↓
Net Worth initialized
```

No balance auto-import from chat/history.

---

# 63. Historical Data

Do not create historical Savings Allocated from old transfer descriptions automatically.

Historical Notion import is outside core V1.

If added later:

```text
old asset tracker
→ historical wealth snapshots
```

not generated transaction rows.

---

# 64. Required API Tests — Existing Regression

Before feature tests, existing tests must keep passing.

Explicitly add regression coverage proving:

```text
manual INCOME still works unchanged
manual EXPENSE still works unchanged
confirm lifecycle unchanged
void lifecycle unchanged
transaction list filters unchanged
evidence endpoint unchanged
audit endpoint unchanged
existing Bank Email expense creation unchanged
existing neutral outgoing transfer unchanged
existing transfer review actions unchanged
existing payslip PRIMARY flow unchanged
existing ORDINARY_INCOME flow unchanged
existing salary cycle boundaries unchanged
existing Telegram record_transaction unchanged
existing Telegram query_cashflow unchanged
existing reconciliation merge/unmerge unchanged
```

---

# 65. Required Tests — Transaction Purpose

Test:

```text
old TRANSFER -> INTERNAL_TRANSFER
old INCOME -> GENERAL
old EXPENSE -> GENERAL
old REFUND -> GENERAL
old ADJUSTMENT -> GENERAL
old UNCLASSIFIED -> GENERAL

invalid type/purpose rejected
saving purpose requires TRANSFER
saving purpose requires related Wealth Account
cross-household Wealth link rejected
```

---

# 66. Required Tests — Savings

Test:

```text
RDN contribution -> savings + amount, expense +0
Gold purchase -> savings + amount, expense +0
explicit savings transfer -> savings + amount
internal transfer -> savings +0
debt principal -> savings +0
pending -> +0
needs review -> +0
voided -> +0
```

---

# 67. Required Tests — Wealth Account

Test:

```text
OWNER create
MEMBER create forbidden
same-household owner
cross-household owner rejected
same-household linked account
cross-household linked account rejected
duplicate active linked account rejected
side/type validation
liability usage validation
immutable side/type
deactivation
historical snapshots survive deactivation
```

---

# 68. Required Tests — Snapshot

Test:

```text
first full snapshot
missing active item rejected
duplicate item rejected
cross-household item rejected
inactive account rejected
zero accepted
negative rejected
string precision
same observedAt conflict
latest deterministic
history ordering
PUT correction atomic
observedAt immutable
no fake zero Net Worth
```

---

# 69. Required Tests — Mutual Fund

Fixture:

```text
previous value = 39m
contribution = 3m
current value = 42.7m
```

Assert:

```text
SavingsAllocated includes 3m
current wealth uses 42.7m
valuation/other can include 0.7m
0.7m is never SavingsAllocated
```

---

# 70. Required Tests — Cycle Residual

Cycle:

```text
Income 16m
Expense 9m
Savings 6m
```

Assert:

```text
RawResidual = 1m
```

When next primary salary closes cycle:

```text
one residual case
one active review
```

Retry job:

```text
still one case
still one review
```

Ordinary income:

```text
must not close cycle
must not create residual review
```

No positive residual:

```text
no review
```

Negative residual:

```text
no review
```

---

# 71. Required Tests — Residual Resolution

Test:

```text
1m -> Jago
```

creates:

```text
cycle_residual_allocation 1m
```

and:

```text
no transaction row
no savings change
no expense change
```

Split:

```text
700k Jago
300k Cash
```

sum accepted.

Sum not equal residual:

```text
reject
```

Cross-household Wealth Account:

```text
reject
```

Concurrent second resolution:

```text
409
```

---

# 72. Required Tests — Residual Staleness

Create case at:

```text
residual 1m
```

Then add historical expense:

```text
700k
```

Before residual resolution recompute:

```text
new residual 300k
```

Old 1m allocation must be rejected.

Review remains/open refreshed for 300k.

If new residual becomes 0:

```text
review cancelled/resolved as no longer needed
```

No stale allocation accepted.

---

# 73. Required Tests — Salary Safety

Force residual-review enqueue failure.

Assert:

```text
PRIMARY salary transaction is still CONFIRMED
salary_event still exists
salary review resolution succeeds
```

Then run catch-up.

Assert:

```text
residual case/review created later exactly once
```

This test is mandatory.

---

# 74. Required Tests — Telegram

Test:

```text
3 juta top up RDN
```

does not guess source.

Test deterministic source/destination.

Test existing matching bank event:

```text
no duplicate transaction
```

Test:

```text
bulan ini udah nabung berapa?
```

uses deterministic backend.

Test:

```text
net worth sekarang?
```

uses latest snapshot and observed timestamp.

Test residual reply:

```text
700k Jago, 300k Cash
```

only creates residual allocation.

---

# 75. Required Tests — Web

Test:

```text
Kekayaan navigation
empty setup state
Wealth Account settings
opening snapshot
visible previous-value prefill
full snapshot required
Net Worth summary
asset/liability grouping
history chart mapping
cycle savings
cycle recap
residual open badge
residual allocation form
no fake balance
mobile navigation
```

---

# 76. CI

Run current repository CI-equivalent checks:

```text
goose migrations
API go test ./...
Worker go test ./...
API go vet ./...
Worker go vet ./...
npm build
npm test
docker compose config
production image builds
native-only LLM check
```

No feature considered done with failing existing tests.

---

# 77. Documentation

Add:

```text
docs/adr/ADR-036-wealth-savings-cycle-reconciliation.md
```

ADR must record:

```text
transaction remains cashflow ledger
purpose derives Savings Allocation
Wealth Account is separate domain
full point-in-time snapshots
no wealth_movement V1
cycle residual attribution is non-ledger metadata
residual review reuses universal Review
salary flow remains authoritative
```

Update:

```text
AGENTS.md
README.md
```

because current AGENTS MVP scope still says assets are not implemented.

Do not delete old historical product docs.

---

# 78. Implementation Order

## Phase A — Guardrails first

```text
ADR-036
regression tests for stable flows
```

## Phase B — Schema

```text
00046
purpose
Wealth Accounts
snapshots
residual case/allocation
review extension
```

## Phase C — Backend domain

```text
wealth package
analytics helpers
Wealth APIs
cycle recap
```

## Phase D — Existing flow integration

```text
manual transfer extension
transfer review purpose mapping
known-account wealth link
primary salary post-commit residual job
```

## Phase E — Web

```text
Settings Wealth Accounts
/wealth
snapshot UI
Overview
cycle recap
residual Inbox UI
```

## Phase F — Telegram

```text
transfer tools
savings query
wealth query
cycle residual review actions
```

## Phase G — hardening

```text
full regression
idempotency
concurrency
docs
```

---

# 79. Explicit Non-Goals

Do not implement:

```text
live mutual fund NAV
live gold pricing
broker API
portfolio security positions
cost basis
realized P&L
unrealized P&L
TWR
XIRR
crypto sync
multi-currency
tax
goals
forecasting
investment advice
wealth_movement table
Savings virtual account
manual salary-cycle close
literal per-rupiah money tracing
```

---

# 80. Forbidden Failure Modes

Implementation is incorrect if any of these can happen:

```text
RDN top-up becomes expense
Gold purchase becomes expense
asset sale becomes income
saving counted twice
snapshot creates transaction
residual attribution creates transaction
residual attribution increases SavingsAllocated
next salary carries previous residual into new Income
new salary is rolled back because residual-review job failed
ordinary income closes salary cycle
LLM chooses household/source account without deterministic proof
same email + Telegram event creates duplicate transaction
stale residual review can be resolved against old amount
cross-household Wealth Account can be linked
inactive Wealth Account silently disappears from historical Net Worth
Net Worth is calculated from transaction contributions instead of snapshot
missing snapshot values silently become zero
```

---

# 81. Final Source-of-Truth Matrix

| Question | Source of truth |
|---|---|
| Income | confirmed transaction |
| Expense | confirmed EXPENSE - REFUND |
| Cashflow Surplus | transaction analytics |
| Intentional Savings | confirmed TRANSFER purpose |
| Saving destination | related Wealth Account |
| Current asset value | latest full Wealth Snapshot |
| Current liability | latest full Wealth Snapshot |
| Net Worth | full Wealth Snapshot |
| Market/other wealth change | snapshot reconciliation residual |
| Salary cycle boundaries | existing confirmed primary salary events |
| Cycle leftover | derived RawResidual |
| Where leftover was retained | cycle_residual_allocation |
| Missing expense/transfer | canonical transaction flow |
| Financial ambiguity | universal Review |
| Integration setup issue | Integration Action |

---

# 82. Final End-to-End Example

Previous cycle:

```text
25 Aug primary salary      +16m INCOME
26 Aug RDN                  3m INVESTMENT_CONTRIBUTION
27 Aug Gold                 1m ASSET_PURCHASE
28 Aug Blu                  2m SAVINGS_TRANSFER
through cycle expenses      9m EXPENSE
```

Cycle metrics:

```text
Income                 16m
Expense                 9m
SavingsAllocated        6m
RawResidual             1m
```

25 Sep new PRIMARY salary:

```text
+16m INCOME
new salary_event
new cycle begins
```

Existing salary commit succeeds independently.

Post-commit idempotent residual job sees previous cycle:

```text
RawResidual 1m > 0
```

Creates:

```text
cycle_residual_case
universal Review
```

User responds:

```text
1 juta masih di Jago
```

Richmod creates:

```text
cycle_residual_allocation
Jago 1m
```

No transaction is created.

Previous cycle recap:

```text
Income                  16m

Expense                  9m
RDN                      3m
Gold                     1m
Blu                      2m
Retained Jago            1m

Total explained         16m
```

New cycle analytics:

```text
Income                  16m
Expense                  0
SavingsAllocated         0
RawResidual             16m
```

The old Rp1m is not carried into new Income.

Physical account value is represented separately by a Wealth Snapshot.

---

# 83. Final Architecture

```text
           FINANCIAL EMAIL / TELEGRAM / DOCUMENT / WEB
                             │
                             ▼
                     SOURCE EVENT / EVIDENCE
                             │
                             ▼
                    CURRENT RICHMOD PIPELINE
                    proposal / reconcile / review
                             │
                             ▼
                    CANONICAL TRANSACTION LEDGER
                             │
                ┌────────────┴────────────┐
                │                         │
                ▼                         ▼
            CASHFLOW               SAVINGS PURPOSE
                │                         │
                │                 related Wealth Account
                │                         │
                └────────────┬────────────┘
                             ▼
                       SALARY CYCLE
                             │
                     cycle closes on
                  next PRIMARY salary
                             │
                             ▼
                    CYCLE RESIDUAL CASE
                             │
                  Universal Review Inbox
                             │
                retained-balance attribution
                             │
                             ▼
                     CYCLE RECAP
                             │

          WEB CURRENT-VALUE INPUT
                  │
                  ▼
             WEALTH SNAPSHOT
                  │
          ┌───────┴────────┐
          ▼                ▼
       ASSETS          LIABILITIES
          │                │
          └───────┬────────┘
                  ▼
               NET WORTH
                  │
                  ▼
       SNAPSHOT RECONCILIATION
```

---

# 84. Definition of Done

This feature is complete only when:

```text
existing stable Richmod flows are retained
Savings Allocation works without expense corruption
Wealth current values are editable on web
full snapshots produce correct Net Worth
salary cycles expose saving destination
positive closed-cycle residual creates non-blocking Review
residual can be split across Wealth Accounts
residual attribution never mutates ledger
new PRIMARY salary never depends on residual job success
late historical transactions cannot silently leave stale residual allocation
Telegram understands saving and residual review safely
financial email dedup/reconciliation remains intact
all tests + CI pass
docs are updated
```

**End of final frozen V1 PRD.**
