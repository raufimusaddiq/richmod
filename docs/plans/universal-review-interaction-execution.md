# Execution Plan — Universal Review Interaction

## Status

PR #159 contract is integrated. UIR-00 audit is merged (PR #162). UIR-01
implementation continues. The first slice (PR #163, merge `72b1342`)
introduced `apps/reviewdomain` and shared locked canonical terminal
transition/projection finalization across the API transaction adapter and
Telegram transaction-review adapter.

The second slice shares household-scoped candidate revalidation
(`ValidateCategoryForHousehold`) and locked subject revalidation
(`ValidateTransactionReview`) so a stale Telegram callback and a stale Web
action fail identically before any transaction mutation.

The third slice moves transaction confirm mutation, proposal/date/merchant
updates, source-event state, optional merchant learning, projection completion,
and terminal review completion into apps/reviewdomain.ConfirmTransactionReview.
Terminal versus partial completion is explicit: Telegram can confirm the
transaction yet keep the review open while asking whether to remember the
merchant; Web resolves in the same transaction. Document extraction and salary
event side effects remain Telegram-specific because Web does not perform them.

The fourth slice moves transaction-review rejection into
apps/reviewdomain.RejectTransactionReview: transaction void, proposal rejection,
source-event refresh, Telegram projection cancellation, and canonical item
completion run once for both Web and Telegram. Web cancels every open projection
for the transaction; Telegram cancels the same way while pinning the exact
bound item.

The fifth slice moves transfer classification into
apps/reviewdomain.ClassifyTransferReview. Web, the bound Telegram adapter, and
the Telegram agent adapter now share category/Wealth validation, transaction and
proposal mutation, source-event refresh, and canonical review completion.
Telegram retains only interaction binding, reply evidence, and surface-specific
messaging; Web retains optional remembered-account creation and its audit.

Still open in UIR-01: duplicate, payslip/date,
financial-email, Wealth, and cycle families. Each family keeps its
residual/document/salary side effects until that whole operation is migrated
with Web/Telegram parity tests. Non-transaction Telegram transition paths
remain specialized for UIR-06/07.

## Source contracts

Read in order:

1. docs/RICHMOD_UNIVERSAL_REVIEW_INTERACTION_PRD.md
2. docs/bdr/BDR-002-review-inbox-channel-independent.md
3. docs/adr/ADR-046-universal-review-interaction-projection.md
4. docs/RICHMOD_INTELLIGENCE_ROUTING_MINIMAL_INTERACTION_PRD.md
5. docs/adr/ADR-039-canonical-review-decision-contract.md
6. docs/UNIVERSAL_REVIEW_INTERACTION_DRIFT_GUARD_CHECKLIST.md

Audited baseline:

~~~text
main = 1f1e7e6a74c4b7bc1fee15e603717edddf707577
~~~

---

# 1. Goal

Make Telegram a complete actionable projection of the canonical Review Inbox for
households that have active Telegram recipients.

Do this without:

- changing canonical financial authority;
- weakening ReviewDecision;
- making reviews implicitly capture normal chat;
- duplicating financial mutation semantics between Web and Telegram;
- regressing PRD #144 model-call ordering.

---

# 2. Current-state audit

## Canonical review domain

Good:

- review_item is universal;
- ReviewDecision carries residual facts/actions;
- Web canonical listing supports non-transaction subjects;
- residual confirmation guards exist;
- RHICE telemetry exists.

## Telegram adapter

Drift:

- review_request was born with transaction_id NOT NULL;
- EnqueueReviewRequest(transactionID, ...) both creates review_item and projection;
- processBoundReview joins transaction;
- reviewInitialState has transaction-centric/default-category assumptions;
- POSSIBLE_DUPLICATE redirects to Web;
- some category-only flows redirect to Web;
- non-transaction review families lack one universal projection path;
- Telegram and Web each contain canonical mutation logic.

This sprint closes that boundary.

---

# 3. Delivery waves

## Wave A — domain foundation

- UIR-00 baseline re-audit;
- UIR-01 shared canonical review resolver;
- UIR-02 universal Telegram projection model.

## Wave B — transaction parity

- UIR-03 ReviewDecision-driven Telegram renderer;
- UIR-04 transaction residual parity;
- UIR-05 duplicate and transfer parity.

## Wave C — universal subject parity

- UIR-06 payslip / source / document reviews;
- UIR-07 financial-email / Wealth / cycle reviews.

## Wave D — synchronization and product gates

- UIR-08 cross-surface synchronization and concurrency;
- UIR-09 telemetry and coverage metrics;
- UIR-10 regression matrix / rollout hardening.

Each task may be a PR, but every review cycle follows repository FIFO discipline:
bundle valid findings into one push, then wait for exact latest head review before
another push.

---

# 4. UIR-00 — baseline re-audit

Before code:

- fetch latest main;
- enumerate every review_type currently permitted by schema;
- identify which types are actually produced;
- map subject type;
- map Web action;
- map Telegram delivery;
- map Telegram completion capability;
- identify legacy/dead review types;
- inspect all canonical mutation paths.

Required artifact:

~~~text
review type
producer
subject
ReviewDecision shape
Web action
Telegram current state
Telegram target state
shared resolver operation
~~~

Exit:

No review type is omitted from the implementation plan.

---

# 5. UIR-01 — shared canonical review resolver

## Objective

Remove channel ownership of financial review mutation.

## Required work

Extract/refactor canonical review operations from current Web and Telegram paths
into channel-neutral Go domain operations.

At minimum support operation families:

- confirm transaction residual;
- set transaction date;
- set category;
- ignore/dismiss;
- merge duplicate;
- confirm new vs duplicate;
- transfer classification;
- salary classification/pay date;
- complete source/bank facts;
- financial-email entity resolution;
- Wealth Account resolution;
- cycle residual policy actions.

One operation may remain internally specialized, but Web and Telegram must call
the same implementation.

## Required invariants

- household authorization;
- review lock;
- active status;
- allowed_actions;
- missing_facts;
- candidate revalidation;
- canonical mutation;
- audit;
- RHICE field attribution;
- idempotency.

## Tests

For representative review families, run the same resolver fixture from:

- WEB input adapter;
- TELEGRAM input adapter;

and assert identical canonical result.

Exit:

No new Telegram review action requires duplicated financial SQL semantics.

---

# 6. UIR-02 — universal Telegram projection model

## Objective

Make review_request/projected conversation capable of representing any review_item.

## Required work

Re-audit schema and choose the smallest safe migration.

Expected direction:

- review_request.review_item_id becomes authoritative;
- transaction_id can become nullable/legacy compatibility field or be removed in
  a later cleanup;
- projection creation accepts review_item ID, not transaction ID;
- review_request review_type is derived/validated from review_item;
- recipient rows stay per Telegram identity;
- conversation references projection/review item without requiring transaction.

Do not make review_item dependent on Telegram.

## Creation contract

~~~text
review_item created/open
-> if eligible Telegram recipient exists
-> ensure one active projection for that review_item
-> create recipient delivery jobs
~~~

Projection creation must be idempotent.

## Backward compatibility

Existing open transaction review_request rows remain resolvable.

Migration must not orphan bound messages.

Exit:

A proposal/source/document/Wealth/cycle review can receive a Telegram projection
without inventing a transaction.

---

# 7. UIR-03 — ReviewDecision-driven Telegram renderer

## Objective

Replace review-type/default-category rendering with a canonical interaction
renderer.

## Renderer input

- ReviewDecision;
- subject summary;
- household-scoped bounded candidates;
- projection state.

## Renderer output

- message text;
- interaction mode;
- callback buttons / pagination;
- expected bound reply field;
- optional View details action.

## Required mappings

- ModeBoundedChoice;
- ModeSingleField;
- ModePolicyChoice;
- ModeConflictResolution;
- compound residual state.

review_type may add wording but not contradict missing_facts/allowed_actions.

## Exhaustiveness

Add a contract test enumerating producible review types.

Adding a new review producer without Telegram renderer capability must fail.

Exit:

No generic "default category" behavior can mis-render a date/policy/duplicate
review.

---

# 8. UIR-04 — transaction residual parity

Cover:

- UNKNOWN_MERCHANT;
- AMBIGUOUS_CATEGORY;
- MISSING_TRANSACTION_DATE;
- TRANSACTION_FACTS_MISSING;
- UNKNOWN_PURPOSE;
- MANUAL_CORRECTION.

## Scenarios

Category only:

- Telegram chooser;
- one input;
- no Web.

Date only:

- bound date reply;
- one input;
- no Web.

Date + category:

- sequential exact binding;
- partial review remains open;
- two human inputs;
- Web reflects current residual after first input.

Unknown purpose/manual correction:

- bound reply;
- shared resolver;
- no unrelated chat capture.

Exit:

No ordinary transaction residual requires Web.

---

# 9. UIR-05 — duplicate and transfer parity

## Duplicate/conflict

Cover:

- POSSIBLE_DUPLICATE;
- CONFLICTING_EVIDENCE.

Telegram must offer current allowed actions.

Candidate selection must be revalidated under lock.

Remove product behavior that says "open Review Inbox" as the only completion
path.

## Transfer

Cover TRANSFER_CLASSIFICATION.

Support:

- expense + category follow-up;
- own account;
- household account;
- investment account;
- asset purchase + Wealth Account follow-up;
- ignore.

Use existing deterministic relationship/Wealth validation.

Exit:

Duplicate and transfer reviews are end-to-end resolvable in Telegram.

---

# 10. UIR-06 — payslip, source, and document parity

Cover currently producible:

- PAYSLIP_CONFIRMATION;
- MISSING_PAY_DATE;
- SALARY_SOURCE_CONFIRMATION if still live;
- UNKNOWN_BANK_TEMPLATE;
- DOCUMENT_EXTRACTION_LOW_CONFIDENCE;
- DOCUMENT_CLASSIFICATION;
- UNKNOWN_EMAIL_TEMPLATE if live;
- RECEIPT_MISMATCH if live;
- INVOICE_PAYMENT_STATUS if live.

For legacy schema values with no producer:

- prove no producer and mark compatibility-only/deprecated;
- still prevent generic invalid rendering if an old open item exists.

Payslip rules must preserve PRD #144:

- existing primary + missing date => date only;
- first salary + date => policy only;
- both missing => both.

Exit:

No active proposal/source/document review silently lacks Telegram capability.

---

# 11. UIR-07 — financial-email, Wealth, and cycle parity

## FINANCIAL_EMAIL_RESOLUTION

- show known resolved entities;
- chooser only for unresolved entity;
- server-known entity contributes zero RHICE;
- partial resolution supported.

## WEALTH_OBSERVATION_CONFIRMATION

- PREPARE_SNAPSHOT;
- SET_WEALTH_ACCOUNT;
- IGNORE;
- account chooser is household-scoped.

## CYCLE_RESIDUAL_ALLOCATION

- ALLOCATE_RETAINED_BALANCE;
- TRANSACTION_MISSING;
- LEAVE_UNALLOCATED;
- subsequent required values collected through exact bound flow.

Exit:

All currently active non-transaction review families have actionable Telegram
projection.

---

# 12. UIR-08 — cross-surface synchronization and concurrency

## Web resolves while Telegram open

Expected:

- canonical review resolves once;
- Telegram buttons become stale;
- message edited to resolved where practical;
- stale callback receives deterministic status.

## Telegram resolves while Web open

Expected:

- Web next read removes/resolves item;
- stale Web POST returns conflict/already resolved;
- no duplicate mutation.

## Multiple Telegram recipients

Expected:

- same review_item;
- first valid action wins;
- siblings become stale;
- one audit resolution.

## Queued delivery after resolution

Before sending an actionable review message, recheck canonical status.

Do not send a fresh live card for an already-resolved review.

Exit:

Cross-surface race cannot duplicate mutation.

---

# 13. UIR-09 — telemetry + Admin Review Operations

## Objective

Make rollout health measurable and operationally visible without PostgreSQL
queries.

Telemetry storage alone does not complete UIR-09.

## Add/derive telemetry

- eligible Telegram reviews;
- projection-created count;
- actionable projection count;
- projection send success/failure/retry;
- resolution surface;
- review type;
- interaction turns;
- stale action attempts;
- Web escape action if explicitly chosen;
- review resolution latency.

Metrics:

### Telegram Actionable Review Coverage

Target 100% after rollout.

### Web Escape Rate

Target 0% for ordinary blockers.

### Completion Surface

TELEGRAM / WEB / SYSTEM.

### Resolution latency

Expose p50 and p95 for time from first actionable projection / canonical review
availability to canonical resolution, with definitions pinned in tests.

Do not alter RHICE semantics.

## Admin API contract

Implement read-only Super Admin aggregates following existing Admin API
conventions.

Required routes:

~~~text
GET /api/v1/admin/reviews/summary?range=24h
GET /api/v1/admin/reviews/breakdown?range=24h
GET /api/v1/admin/reviews/projections?range=24h&status=&reviewType=&q=
~~~

The exact pagination token follows existing Admin list conventions.

### summary response must support

- openReviews;
- eligibleTelegramReviews;
- actionableTelegramProjections;
- telegramActionableCoverageRate;
- webEscapeRate;
- deliveryAttempts;
- deliverySucceeded;
- deliveryFailed;
- deliveryRetried;
- deliverySuccessRate;
- staleActionAttempts;
- resolutionLatencyP50Ms;
- resolutionLatencyP95Ms;
- resolvedByTelegram;
- resolvedByWeb;
- resolvedBySystem.

### breakdown rows must support

- reviewType;
- created;
- open;
- telegramEligible;
- actionableProjected;
- coverageRate;
- resolvedTelegram;
- resolvedWeb;
- resolvedSystem;
- webEscapeRate;
- deliveryFailed.

### projection rows must expose only safe operational metadata

- review/projection reference;
- review type;
- review status;
- projection status;
- delivery status;
- retry count;
- created/updated timestamps;
- age;
- resolution surface;
- safe error class.

Do NOT return amount, merchant, counterparty, email body, document content,
prompt/response text, or other raw financial evidence.

## Admin Web contract

Extend the existing `apps/web/app/admin/page.js` navigation with a dedicated:

~~~text
/admin?tab=reviews
~~~

### Reviews tab

Show:

1. headline metrics:
   - open reviews;
   - TARC;
   - Web Escape Rate;
   - Telegram delivery success;
   - p50/p95 resolution latency;
   - stale action attempts;
   - completion surface split;

2. per-review-type breakdown table;

3. projection/delivery operations table with:
   - range filter;
   - projection/review status filter;
   - review-type filter;
   - safe reference search;
   - pagination using existing Admin patterns.

### Overview integration

Add compact Review health to the existing Admin Overview:

- open reviews;
- TARC;
- Telegram review delivery failures;
- Web Escape Rate.

Do not duplicate the detailed Reviews table on Overview.

### Household detail integration

Extend existing Admin household detail with safe review diagnostics:

- Telegram linked/eligible state;
- open reviews;
- actionable Telegram projections;
- latest delivery failure timestamp/error class;
- completion surface distribution.

If household-level diagnostics require an existing endpoint extension, extend the
current household overview response rather than creating a competing household
review API without need.

## Required tests

API:

- TARC numerator/denominator semantics;
- Web Escape Rate excludes voluntary View details;
- completion surface classification;
- stale action aggregation;
- delivery retry/failure aggregation;
- latency percentile definition;
- per-review-type breakdown catches a single uncovered type;
- sensitive financial fields never appear in Admin review responses;
- Super Admin authorization follows existing Admin protection.

Web:

- Review tab is registered and loads summary/breakdown/projections;
- headline values render null/zero safely;
- filters produce expected Admin API requests;
- Overview renders compact Review health;
- household detail renders review diagnostics;
- no raw financial evidence fields are rendered.

## Exit gate

UIR-09 is incomplete if any rollout/DoD review metric requires manual SQL to
inspect.

---

# 14. UIR-10 — rollout and regression

## Regression matrix

Must include:

- category-only;
- date-only;
- compound;
- unknown purpose;
- possible duplicate;
- conflicting evidence;
- transfer classification;
- asset purchase Wealth selection;
- payslip policy/date combinations;
- bank/document completion;
- financial-email partial entity;
- Wealth observation;
- cycle residual;
- Web-vs-Telegram race;
- multi-recipient race;
- Telegram outage;
- open review + unrelated new chat;
- stale callback;
- no AI call for deterministic callbacks;
- Admin TARC / Web Escape / completion-surface aggregate correctness;
- Admin per-review-type coverage exposes one deliberately uncovered fixture;
- Admin projection diagnostics omit raw financial evidence;
- Admin Overview and Review tab contract tests;
- Admin household review-diagnostic contract.

## Rollout

Possible family switches:

- transaction review projection;
- proposal/document projection;
- Wealth/financial-email/cycle projection.

Switch off means Web remains available.

It must not mutate or discard canonical review state.

## Final exit

Do not call sprint complete until:

1. the exhaustive current review-type coverage test shows every producible type
   is FULL Telegram capability; and
2. the Admin Review surface proves the same rollout metrics without manual
   PostgreSQL inspection.

---

# 15. Task graph

~~~text
UIR-00
  |
  +--> UIR-01 --> UIR-04 --> UIR-05 --+
  |                                   |
  +--> UIR-02 --> UIR-03 -------------+--> UIR-08 --> UIR-09 --> UIR-10
  |                 |                 |
  |                 +--> UIR-06 ------+
  |                 |                 |
  |                 +--> UIR-07 ------+
~~~

UIR-01 and UIR-02 are foundation work.

Do not implement family-specific Telegram handlers by bypassing them.

---

# 16. Task completion template

Every task record must include:

~~~text
Task:
Baseline main SHA:
Review types covered:
Subject classes covered:
Web behavior before/after:
Telegram behavior before/after:
Canonical resolver used:
Human inputs before/after:
Model calls before/after:
Cross-surface behavior:
Tests:
Drift guard:
Known follow-up:
~~~

A task is not complete when Telegram merely receives a notification.

It is complete when the required human decision can finish there through the
canonical resolver.
