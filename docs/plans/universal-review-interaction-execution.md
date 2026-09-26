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

The sixth slice moves possible-duplicate merging into
apps/reviewdomain.MergeDuplicateReview. Web and both Telegram paths (the bound
detail callback and the shared confirmation lane) now share the target
revalidation, reconciliation_merge creation, evidence copy, source void,
proposal merge, source-event refresh, and canonical review completion. Telegram
keeps only its server-owned candidate list in review_conversation.context_json,
the indexed callback binding, and surface messaging; the duplicate review also
reuses the shared confirm operation for "record as new".

The seventh slice moves salary recording into
apps/reviewdomain.RecordSalaryEvent (with shared PromoteEvidenceDocuments and
LoadPayslipFacts helpers). Web payslip confirmation and both Telegram confirm
lanes (the bound reply lane and the conversational agent lane) now share the
salary_source upsert, first-primary promotion, salary_event insert, document
promotion, and payslip-evidence facts. The conversational agent confirm also
now calls the shared ConfirmTransactionReview instead of its own transaction
and proposal mutation SQL.

The eighth slice moves cycle residual reconciliation into
apps/reviewdomain.ApplyCycleResidual (with shared ValidateCycleAllocations).
Web, the bound Telegram reply lane, and the conversational agent lane now share
the cycle basis recomputation, positive-residual staleness refresh,
no-longer-applicable closure, allocation validation, allocation insert, and
review completion. Each surface keeps only its own transport, wording, and
audit action naming.

The ninth slice moves Wealth observation resolution into
apps/reviewdomain (ResolveWealthObservation, DismissWealthObservation,
ValidateWealthAccount, ReclassifyWealthEvidence). The Review Inbox and every
Telegram wealth lane now share the account validation, review-learned alias
write, observation dismissal, and evidence reclassification. The alias write
now refuses to overwrite a USER-authored alias, which the previous Web-only
path allowed.

The tenth slice moves financial email entity resolution into
apps/reviewdomain.ResolveFinancialEmailEntities, with shared LearnEntityAlias
and LearnEntityAliasIfNew helpers. The Review Inbox financial-email handler now
calls them instead of its inline merge, validation, and alias SQL, so the
already-resolved-entity rules are channel-neutral and ready for the UIR-07
Telegram surface (which has no financial-email handler today). That closes the
UIR-01 operation families.

The eleventh slice starts UIR-03: a ReviewDecision-driven Telegram renderer
(apps/worker/internal/telegram/review_render.go). EnqueueReviewRequest now
renders the stored decision instead of branching on review_type, so the
unresolved fact selects the prompt and the allowed actions select the markup:
a category-only decision gets the bounded chooser, a date/policy/correction
decision gets a bound reply, and a duplicate decision gets the duplicate
intents. A decision-less review fails closed to a bound detail prompt instead
of the old default category chooser. A contract test enumerates every
producible review type and fails if a review can render as a category chooser
without a category-only decision.

The renderer also routes the unresolved dimension to the resolver that can store it: a 
date-only decision now renders an `AWAITING_DATE` bound reply (migration 
00068) that writes `transaction_at` on the transaction and proposal and completes the 
review when no category is left. Before this, every `AWAITING_DETAIL` reply was saved as a 
description, so a date-review could never store its date. A DB-backed integration 
test covers the full bound-reply path. Transfer reviews also now render the 
transfer chooser instead of falling through to the generic keyboard.

The twelfth slice starts UIR-04: a review whose stored ReviewDecision leaves
only the category unresolved is now completable inside Telegram instead of
sending the user to the Review Inbox. The old bound-reply branch keyed off
`reviewType == "UNKNOWN_MERCHANT"`, so an `AMBIGUOUS_CATEGORY` review (same
category-only contract, different reason code) fell through to the
conversational LLM lane and could not be answered deterministically. Both
`processBoundReview` and the category pager callback now gate on
`missingFactsAreCategoryOnly`, so any category-only decision offers the paged
chooser: a bound text reply sends a fresh chooser message, while a pager
callback edits the existing chooser in place. A legacy review with no stored
contract keeps its previous behavior. The MANUAL_CORRECTION payslip notice no
longer tells the user to open Review Inbox, since the bound reply lane handles
it. DB-backed tests cover UNKNOWN_MERCHANT and AMBIGUOUS_CATEGORY bound replies
and the chooser pager callback.

The same slice closes the free-form residual families. A description reply to
an `UNKNOWN_PURPOSE` or `MANUAL_CORRECTION` review used to be saved and then
park the conversation in `AWAITING_CATEGORY`, so a salary correction or a
purpose review could never finish from Telegram. The description save now
completes through the shared `resolveReviewTx`/`ConfirmTransactionReview` when
the transaction needs no category, and otherwise continues to the chooser
instead of attempting a confirm that the canonical expense-category invariant
would reject. This keeps MANUAL_CORRECTION (a payslip income transaction) a
one-reply review while an uncategorized `UNKNOWN_PURPOSE` expense still
resolves through the chooser. The plan doc's UIR-04 family list is therefore
covered for the Telegram bound-reply channel.

The thirteenth slice starts UIR-05: duplicate and transfer parity. A typed reply
to a `TRANSFER_CLASSIFICATION` review no longer falls into the `AWAITING_DETAIL`
description saver; a stored `transfer_relationship` decision now routes to a
dedicated `classifyTransferReply`, which maps one of the bounded intents
(expense, own account, household account, investment account, asset purchase,
ignore) through the shared `reviewdomain.ClassifyTransferReview`. The transfer
chooser keyboard follows the transaction type, so an expense review offers only
the classifications the canonical rule accepts (expense, asset purchase) rather
than buttons that would produce a stale-action reply. Transfer buttons delivered
as callbacks (`review:expense`/`:own`/`:household`/`:asset`) now continue into the
bound-review lane instead of being rejected as completed actions, and an expense
transfer review offers the category chooser from Telegram rather than the Review
Inbox. POSSIBLE_DUPLICATE already offered its candidate chooser; a DB-backed test
now drives the full `Process` path (bound reply records candidates, callback
merges and voids the source) to prove it completes without Web. `CONFLICTING_EVIDENCE`
is produced on a source-event subject by financial email and has no transaction
projection, so its Telegram surface belongs to UIR-07.

### UIR-02 — universal projection model (complete; PR #179, merged 2026-09-26)

Every producer that inserts a `review_item` now routes its Telegram delivery
through one shared entrypoint, `telegram.ProjectReviewItem` (plus the
message-free `ProjectReviewMessage`), instead of hand-rolling a notice or
leaving the item Inbox-only. The producer writes the item and its
`ReviewDecision` as before; the shared projection then resolves eligible
recipients (falling back to the originating Telegram chat for a source with no
linked household chat), picks the markup from the decision, and enqueues the
message. Transaction creation, receipt/payslip/document extraction, financial
provider email, bank email, and cycle residual all route through it, so a
document, wealth, financial-email, or cycle review arrives as the same
decision-driven actionable card as a transaction review. Projection creation is
idempotent: an open `review_request` for the item is reused, and an
already-open item is reused instead of duplicated.

`renderReviewPresentation` now guarantees a producer-supplied prompt keeps the
decision's conversation state and markup, so a supplied message still lands on
the right interaction. Existing category, date, transfer, duplicate, and policy
rendering is unchanged; `TestSuppliedContextKeepsItsMarkupMode` pins the
supplied-prompt behavior.


The slice also added migration 00069, which widens the
review_request.review_type CHECK to the review_item reason set; without it a
document, payslip, bank, or financial-email projection failed the constraint
(SQLSTATE 23514) and the review never reached Telegram.


The remaining channel gaps are subject-parity work: the rest of UIR-03
action parity, UIR-04 transaction residual parity, UIR-06
payslip/source/document parity, and UIR-07 financial-email/Wealth/cycle
Telegram surfaces. The first UIR-06 slice extracts proposal-backed payslip
confirmation from the Web handler into `reviewdomain.ResolvePayslipProposal`;
it does not yet provide Telegram parity or close UIR-06. UIR-08
through UIR-10 cover synchronization, telemetry, and the regression matrix.

### UIR-06 — payslip, source, and document parity (complete; PR open)

Two slices close UIR-06.

Payslip parity. `PAYSLIP_CONFIRMATION` and `MISSING_PAY_DATE` now finish inside
Telegram through the shared `reviewdomain.ResolvePayslipProposal`. The policy
chooser (`review:salary:primary` / `:ordinary`) stores the salary classification
as a `knownFacts` residual via `reviewdomain.SetPayslipPolicy` and advances the
conversation to `AWAITING_DATE`; a bound date reply resolves the same proposal
into a canonical income transaction and, for a primary salary, records the
salary event through `reviewdomain.RecordSalaryEvent`. The pay-date parser
accepts an unprefixed `25 September 2026` as well as the labelled form, while
the transaction-date prompt keeps its `YYYY-MM-DD` parser. Legacy unbound
payslip items (no `document_id`, metadata without a document binding) resolve
through a fail-closed `documentMatches` guard instead of a hard `JOIN document`,
which had made `FOR UPDATE` illegal on the nullable side of a `LEFT JOIN`
(SQLSTATE 0A000) and surfaced as "no rows".

Document parity. `DOCUMENT_CLASSIFICATION` and
`DOCUMENT_EXTRACTION_LOW_CONFIDENCE` — the terminal-failure reviews the shared
document pipeline raises for a payslip, receipt, or document it cannot classify
or extract — now resolve through the shared `reviewdomain.ResolveDocumentReview`.
Their bounded actions are `REPROCESS_DOCUMENT` (re-run the document pipeline) and
`IGNORE` (park the document), replacing the old `COMPLETE_BANK_FACTS` action that
could never apply: a document-bound item has no `bank_email_extraction` row and
no required `source_event_id`, so the bank-fact completion path and the Web
branch both rejected it. The Review Inbox action list, a new
`review:reprocess` Telegram callback, and the decision-driven renderer's
`document` markup mode all route to the one resolver, so the card always has a
button that can finish the review. `TelegramCompletableReviewType` now admits
both families, so the previously dead-ended classification card is projected.

Still no producer in the active path: `SALARY_SOURCE_CONFIRMATION`,
`UNKNOWN_BANK_TEMPLATE`, `UNKNOWN_EMAIL_TEMPLATE`, `RECEIPT_MISMATCH`, and
`INVOICE_PAYMENT_STATUS`. They remain schema compatibility values with renderer
fallbacks; the exhaustive renderer contract test keeps any future producer from
reaching Telegram without a completable path.

### UIR-07 — financial-email, Wealth, and cycle parity (complete; PR open)

`WEALTH_OBSERVATION_CONFIRMATION` (UIR-01 slice 9) and
`CYCLE_RESIDUAL_ALLOCATION` (slice 13) already resolve in Telegram through the
special and residual lanes. The remaining active family,
`FINANCIAL_EMAIL_RESOLUTION`, now resolves from Telegram too: the shared
`reviewdomain.ResolveFinancialEmailEntities` is reached from a household-scoped
entity chooser (`review:fe:account:` / `review:fe:wealth:`), which offers only
the dimension the stored decision still names as missing. A partial resolution
is persisted on the observation columns — the resolver's own source of truth —
before the card re-asks for the remaining entity, so a two-entity provider email
finishes in two taps. Completion enqueues the `PROCESS_FINANCIAL_EMAIL` replay,
matching the Review Inbox. The renderer gained a `financial_email` markup mode
and the family was added to `TelegramCompletableReviewType` and the tool
registry, so the card is projected instead of fail-closed.

### UIR-08 — cross-surface synchronization and concurrency (complete; PR open)

The canonical resolve path already serializes on `FOR UPDATE` and completes a
review only when `RowsAffected=1` on `OPEN|PENDING_SEND`, so a Web action and a
Telegram callback for the same item cannot both mutate: the loser sees an
already-resolved review. Web's next read drops the resolved item, a stale Web
POST returns 409 (pinned by the residual integration test), and a stale Telegram
callback answers deterministically. Multiple recipients share one `review_item`,
so the first valid action wins and siblings go stale.

The one missing piece was queued delivery after resolution. A `SEND_TELEGRAM_MESSAGE`
job that carries a `review_request_id` now rechecks `ReviewProjectionOpen` before
sending: a review resolved, cancelled, or expired between enqueue and send is not
delivered as a fresh live card (a pending callback is still answered so the
client spinner clears). `TestQueuedReviewSendSkipsResolvedProjection` pins the
open/resolved/expired/empty cases.

### UIR-09 — telemetry + Admin Review Operations (complete; PR open)

Rollout health is measurable and viewable without PostgreSQL. Three read-only
Super Admin aggregates back the Admin Web Reviews tab:

- `GET /api/v1/admin/reviews/summary` — open reviews, Telegram-eligible open
  reviews, actionable projections, TARC, Web Escape Rate, delivery
  attempts/success/failure/retry, stale action attempts, p50/p95 resolution
  latency, and the TELEGRAM/WEB/SYSTEM completion-surface split.
- `GET /api/v1/admin/reviews/breakdown` — the same signals per review type.
- `GET /api/v1/admin/reviews/projections` — paginated projection/delivery rows
  exposing only safe operational metadata (reference, statuses, delivery state,
  retry count, timestamps, age, resolution surface). No amount, merchant,
  counterparty, email body, document content, or prompt text is ever selected.

The Web Admin Reviews tab (`/admin?tab=reviews`) renders the headline metrics,
the per-type table, and the filterable/paginated projections table; the Overview
shows compact Review health (open reviews, TARC, delivery failures, Web Escape
Rate) sourced from the same summary endpoint. Projection `updatedAt` uses
`COALESCE(resolved_at,created_at)` because `review_request` has no `updated_at`.
Admin household detail (`GET /api/v1/admin/households/{id}/overview`) now also
returns a `reviewDiagnostics` block (Telegram-eligible members, actionable
projections, TELEGRAM/WEB/SYSTEM completion split, latest delivery failure
timestamp and error class) so an operator can diagnose one household's Telegram
review delivery/actionability without SQL; a query failure omits the block
rather than failing the overview.

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

## Status

Complete. All listed operation families have a shared implementation that every
existing surface calls; the slice history above records each one. Financial
email resolution currently has only the Review Inbox surface, so its shared
operation is ready for the UIR-07 Telegram projection but that projection is
still outstanding.

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

## Status

Complete. PR #179 merged into `main` on 2026-09-26 at
`9dd61833c133575193f75a4834c137c742332ce4`. Reviewer findings were addressed
before approval. Family-specific resolver parity remains tracked by UIR-06 and
UIR-07; projection coverage alone does not complete those tasks.

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
