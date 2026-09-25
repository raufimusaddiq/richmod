# RICHMOD — UNIVERSAL REVIEW INTERACTION

## Product Requirements Document for Codex /goals

**Status:** APPROVED PRODUCT DIRECTION / NEXT SPRINT CONTRACT  
**Repository:** raufimusaddiq/richmod  
**Audited implementation baseline:** main @ 1f1e7e6a74c4b7bc1fee15e603717edddf707577  
**Date:** 2026-09-25  
**Depends on:** PRD #144 intelligence-routing contract  
**Product decision:** BDR-002  
**Architecture:** ADR-046, amending ADR-023 / ADR-028 / ADR-044  
**Backend:** Go  
**Frontend:** Next.js + React + JavaScript  
**Canonical store:** PostgreSQL  
**Primary conversational surface:** Telegram  
**Rich inspection surface:** Web Review Inbox  

---

# 0. Normative status and precedence

This PRD defines how human review is exposed and resolved across Richmod
interaction surfaces.

It does not replace the intelligence-routing PRD. The two contracts compose:

~~~text
PRD #144:
minimize whether a human must be involved

this PRD:
when a human is required, let them finish from the surface they already use
~~~

For universal review interaction work, precedence is:

1. this PRD;
2. BDR-002;
3. ADR-046;
4. the PRD #144 intelligence-routing contract;
5. ADR-039 ReviewDecision;
6. ADR-023 / ADR-028 / ADR-044 as amended by ADR-046;
7. older review / Telegram implementation documents.

Execution plan:

- docs/plans/universal-review-interaction-execution.md

Per-task drift gate:

- docs/UNIVERSAL_REVIEW_INTERACTION_DRIFT_GUARD_CHECKLIST.md

## 0.1 /goals execution rules

Codex MUST:

1. fetch current main before implementation;
2. verify this baseline remains an ancestor or re-audit if main moved;
3. read this PRD, BDR-002, ADR-046, PRD #144, ADR-039, and the execution plan;
4. treat review_item as the canonical review backlog;
5. treat Web and Telegram as projections over the same ReviewDecision;
6. preserve route-first Telegram behavior so open reviews cannot swallow unrelated
   chat messages;
7. preserve exact callback/reply binding for review interactions;
8. implement review resolution through shared Go domain logic rather than
   duplicating financial mutation logic per channel;
9. test both channels against the same canonical resolution outcomes;
10. run the drift guard before marking each task complete.

A task is incomplete if Telegram displays a review but still requires Web for the
ordinary blocking decision.

---

# 1. Product problem

Richmod already made two important architectural decisions:

~~~text
review_item = canonical human-review backlog
ReviewDecision = canonical explanation of what is known and unresolved
~~~

The Web Review Inbox consumes this universal backlog.

Telegram does not yet do so universally.

Current audited implementation has several historical constraints:

- review_request began as a transaction-only Telegram adapter;
- EnqueueReviewRequest accepts a transaction ID and creates transaction review
  state directly;
- processBoundReview joins review_request to transaction;
- non-transaction review_item subjects can exist without a Telegram projection;
- POSSIBLE_DUPLICATE explicitly tells the user to open Review Inbox;
- some category reviews redirect the user to Web even though the unresolved
  choice is already bounded;
- proposal, source-event, document, Wealth, financial-email, and cycle reviews
  are not handled through one universal Telegram interaction path;
- Web and Telegram contain separate pieces of review mutation behavior.

This produces the wrong experience:

~~~text
user uses Telegram
-> Richmod intelligently processes the event
-> one irreducible human decision remains
-> Richmod says "open the website"
~~~

That violates the reason Telegram is a first-class Richmod interface.

---

# 2. Product thesis

> Review Inbox is not a web page. Review Inbox is a canonical decision backlog.

Web and Telegram are interaction surfaces over that same backlog.

For a household with an active Telegram identity:

> Every active human review must have an actionable Telegram projection.

"Actionable" means the user can complete the minimum decision required to move
the canonical review forward without opening Web.

Web remains valuable as the richest inspection and bulk-management surface.

Telegram becomes the fastest decision surface.

~~~text
Telegram = fastest path to decision
Web      = richest path to inspection
~~~

Neither surface owns canonical financial truth.

Go/PostgreSQL do.

---

# 3. Relationship to PRD #144

PRD #144 established:

~~~text
correctness
then minimum human interaction
then minimum intelligence work
~~~

This PRD adds a channel-friction rule:

~~~text
when human interaction is irreducible,
do not add an unnecessary surface switch
~~~

Opening Web is itself interaction friction.

Therefore:

~~~text
one bounded Telegram decision
is preferable to
one notification + opening Web + finding the review + making the same decision
~~~

This sprint MUST NOT increase RHICE merely because Telegram is added.

It reduces channel-switch cost, not financial correctness requirements.

---

# 4. Canonical architecture

Required conceptual model:

~~~text
source / intelligence
        |
        v
   ReviewDecision
        |
        v
    review_item
   CANONICAL BACKLOG
        |
        +------------------------+
        |                        |
        v                        v
 Telegram projection       Web projection
        |                        |
        +-----------+------------+
                    |
                    v
         canonical review resolver
                    |
                    v
              Go validation
                    |
                    v
               PostgreSQL
~~~

The channels render and collect interaction.

They do not independently define financial resolution policy.

---

# 5. Core product invariants

## 5.1 Canonical backlog

Every human review opportunity is represented by review_item.

A Telegram delivery failure MUST NOT delete, resolve, or prevent creation of the
canonical review.

Web visibility MUST NOT depend on Telegram health.

## 5.2 Actionable Telegram projection

If the household has an eligible active Telegram recipient, every open review
requiring human input MUST receive a Telegram projection that can complete the
ordinary blocking decision.

A message that only says:

~~~text
Open Review Inbox to continue
~~~

does NOT satisfy this requirement.

A Web link may exist as a secondary "View details" action.

It must not be the primary completion path for current supported review types.

## 5.3 Shared resolution semantics

Web and Telegram MUST resolve through the same Go review-domain rules.

Forbidden:

~~~text
Web mutation implementation A
Telegram mutation implementation B
~~~

Required:

~~~text
Web interaction ------+
                      +-> ReviewResolutionCommand -> shared resolver -> Go/PostgreSQL
Telegram interaction -+
~~~

Channel-specific code may parse callbacks, replies, pagination, and presentation.

It MUST NOT reimplement canonical financial rules.

## 5.4 ReviewDecision drives presentation

Both surfaces render from the same canonical decision:

- known_facts;
- proposed_facts;
- missing_facts;
- conflicting_facts;
- bounded_choices;
- allowed_actions;
- interaction_mode;
- why_not_auto_confirm.

A Telegram renderer MUST NOT infer a different missing field from review_type
when the stored ReviewDecision says otherwise.

## 5.5 Exact binding without chat capture

An explicit Telegram callback or reply to a review message is an exact binding.

An open review by itself is NOT permission to consume unrelated future chat.

Existing route-first implicit binding behavior remains mandatory.

## 5.6 First valid resolution wins

A review may be visible on Web and on multiple Telegram recipients.

The first valid canonical resolution wins atomically.

Later callbacks/replies must return a deterministic stale/resolved response and
must never create a second mutation.

---

# 6. Telegram product behavior

Telegram review interaction has four building blocks.

## 6.1 Bounded buttons

Use buttons when ReviewDecision exposes a small bounded choice.

Examples:

- category;
- salary classification;
- transfer relationship;
- duplicate relationship;
- ignore / accept;
- residual allocation choice.

## 6.2 Bound reply

Use a reply to the specific review message when the user must provide a scalar or
short free-form fact.

Examples:

- transaction date;
- missing transaction purpose;
- correction detail.

The reply remains bound to review_item through projection state.

## 6.3 Paged chooser

Use paged callbacks for larger server-owned candidate sets.

Examples:

- categories;
- accounts;
- Wealth Accounts;
- duplicate candidates.

Candidate IDs remain server-side.

Telegram callback payloads must use opaque or short-lived references rather than
expose trusted canonical identifiers as model-decided values.

## 6.4 Sequential residual collection

When N facts are genuinely unresolved, Telegram may ask them sequentially.

Example:

~~~text
missing_facts = ["transaction_at", "category"]

1. "Tanggal transaksinya kapan?"
2. user replies
3. "Pilih kategori"
4. user taps category
5. review resolves
~~~

RHICE remains 2 because two facts were required.

Telegram does not turn two genuine inputs into one merely for metric optics.

---

# 7. Review coverage contract

Every current review type in the database must be audited.

No review type may silently fall through to a generic Web redirect.

## 7.1 Transaction category reviews

Types:

- UNKNOWN_MERCHANT;
- AMBIGUOUS_CATEGORY.

Target Telegram interaction:

- show amount/date/description or merchant if known;
- show why category is unresolved;
- render category choices;
- allow Ignore;
- optional Change details / View details secondary action.

Merchant remains optional when ReviewDecision does not name merchant as missing.

## 7.2 Missing transaction date

Type:

- MISSING_TRANSACTION_DATE.

Target:

- show known amount/category/merchant;
- ask only transaction date;
- accept a reply safely parsed to a canonical date;
- confirm only after shared resolver validates residual closure.

No Web requirement.

## 7.3 Compound transaction facts

Type:

- TRANSACTION_FACTS_MISSING.

Target:

- collect only the stored residual dimensions;
- support date + category sequentially;
- preserve known facts;
- resolve after all canonical-required residuals close.

## 7.4 Unknown purpose / manual correction

Types:

- UNKNOWN_PURPOSE;
- MANUAL_CORRECTION.

Target:

- bound reply for the exact requested detail;
- use bounded choices where ReviewDecision supplies them;
- never open a generic conversational mutation surface just because the user is
  in a review.

## 7.5 Duplicate / conflicting evidence

Types:

- POSSIBLE_DUPLICATE;
- CONFLICTING_EVIDENCE.

Target Telegram interaction:

~~~text
Possible match:
Rp57.500 · Indomaret · 23 Sep

[Merge with this]
[Record as new]
[Ignore]
[View details]
~~~

When multiple candidates exist, page or list bounded candidates.

"Open Review Inbox" is no longer a valid primary outcome.

Duplicate safety remains fail-closed.

## 7.6 Transfer classification

Type:

- TRANSFER_CLASSIFICATION.

Target:

- expense;
- own account;
- household account;
- investment account;
- asset purchase;
- ignore;

subject to the stored allowed_actions.

If a follow-up Wealth Account or category is required, Telegram collects that
residual with a paged chooser.

## 7.7 Payslip policy

Types:

- PAYSLIP_CONFIRMATION;
- MISSING_PAY_DATE;
- SALARY_SOURCE_CONFIRMATION if still producible.

Target:

- PRIMARY_SALARY / ORDINARY_INCOME / IGNORE when policy is unresolved;
- date-only bound reply when only date is missing;
- sequential date + salary policy when both are unresolved;
- no salary-policy question when household state already resolves it.

## 7.8 Bank / document evidence completion

Types:

- UNKNOWN_BANK_TEMPLATE;
- DOCUMENT_EXTRACTION_LOW_CONFIDENCE;
- DOCUMENT_CLASSIFICATION;
- UNKNOWN_EMAIL_TEMPLATE;
- RECEIPT_MISMATCH;
- INVOICE_PAYMENT_STATUS if still producible.

Target:

- render the exact ReviewDecision residuals;
- collect only required supported facts;
- support sequential bound replies / bounded actions as needed;
- if a legacy review type has no current producer, explicitly deprecate it or
  implement renderer coverage; do not leave an untested Web-only hole.

## 7.9 Financial Provider Email

Type:

- FINANCIAL_EMAIL_RESOLUTION.

Target:

- display any already-resolved account/entity;
- ask only unresolved account or Wealth Account;
- use server-owned paged chooser;
- never ask the user to reselect known entity IDs;
- complete through the shared canonical resolver.

## 7.10 Wealth observation

Type:

- WEALTH_OBSERVATION_CONFIRMATION.

Target:

- show institution/account hint/value/date;
- allow PREPARE_SNAPSHOT;
- allow SET_WEALTH_ACCOUNT with Telegram chooser;
- allow Ignore;
- if snapshot preparation creates additional canonical review work, that work is
  also projected to Telegram.

## 7.11 Cycle residual allocation

Type:

- CYCLE_RESIDUAL_ALLOCATION.

Target:

- Allocate retained balance;
- Transaction missing;
- Leave unallocated;

with subsequent account/value collection only if that action actually requires
it.

This is human policy and must remain user-owned.

---

# 8. Capability policy

For current review types, target Telegram capability is:

~~~text
FULL
~~~

No current review type is pre-approved as Web-only.

Future review types MUST declare an interaction capability before merge.

Allowed future capability states:

- FULL;
- TEMPORARILY_BLOCKED_BY_EXPLICIT_PRODUCT_EXCEPTION.

There is no silent default to Web.

Any exception MUST include:

- reason;
- affected review type;
- why Telegram cannot safely represent the decision;
- explicit product approval;
- fallback behavior;
- removal criterion.

"Not implemented yet" is not a valid product exception after this sprint exits.

---

# 9. Projection lifecycle

## 9.1 Creation

When review_item becomes active:

1. canonical review commits;
2. eligible Telegram recipients are resolved;
3. one projection/request is created for the canonical review;
4. delivery jobs are queued.

Telegram availability is not part of the canonical transaction.

## 9.2 Delivery failure

If Telegram send fails:

- review_item remains open;
- projection retains retryable delivery state;
- Web remains usable;
- job retries follow normal queue policy;
- failure is observable.

Do not roll back canonical review creation because Telegram is down.

## 9.3 Resolution from Telegram

When Telegram resolves:

- shared resolver locks review_item;
- validates action/values/current state;
- performs canonical mutation;
- resolves review_item;
- marks projection resolved;
- Web no longer shows active item;
- sibling Telegram projections become stale/resolved.

## 9.4 Resolution from Web

When Web resolves:

- same shared resolver runs;
- review_item resolves;
- Telegram projection must no longer present live actions;
- queued stale delivery is cancelled or rendered as resolved;
- already-sent Telegram message should be edited when practical to say the
  review was completed elsewhere.

## 9.5 Expiration

Projection expiration MUST NOT silently expire canonical review work unless
product policy explicitly says the review itself expires.

A Telegram request TTL is a delivery/conversation property.

The canonical review backlog remains authoritative.

---

# 10. Recipient policy

Preserve household authorization.

Eligible recipient order should remain deterministic:

1. preferred_user_id when linked and active;
2. household owner;
3. other active authorized members according to existing policy.

Multiple recipients may see the same review.

They do not create multiple canonical review rows.

Resolution is idempotent and first-valid-write wins.

Do not send household financial reviews to Telegram identities that are not
currently active household members.

---

# 11. Conversation behavior

Telegram must remain a conversational finance interface while acting as an inbox.

Required:

- exact callback/reply binding wins;
- exact review reply is scoped to that review;
- an implicit open review does not own unrelated messages;
- ordinary new transaction text still routes normally;
- many open reviews do not shrink the general tool catalog;
- user can ask "review yang belum selesai apa?" and receive a bounded summary;
- user may request the next pending review without Web.

Optional future UX:

~~~text
/review
"review berikutnya"
"ada yang perlu aku cek?"
~~~

The sprint may add a natural review-list/read path if it uses canonical
review_item state.

---

# 12. Shared canonical resolver contract

The implementation should converge on one Go domain service.

Conceptually:

~~~text
ResolveReview(
  household,
  user,
  reviewItemID,
  action,
  suppliedValues,
  sourceSurface
)
~~~

The exact API/type name is implementation-specific.

The resolver owns:

- review row lock;
- household/user authorization;
- allowed action validation;
- residual missing-fact validation;
- category/account/wealth-account membership validation;
- duplicate candidate validation;
- transfer classification constraints;
- salary policy constraints;
- canonical transaction/proposal/source/document mutation;
- audit;
- RHICE telemetry;
- resolution status.

Channel handlers own:

- Telegram callback/reply decoding;
- Web request decoding;
- presentation;
- paged interaction state;
- user-facing wording.

A Telegram handler MUST NOT duplicate SQL-heavy financial transition logic that
the Web resolver implements separately.

---

# 13. Presentation contract

Telegram should be concise but decision-complete.

Every review message should answer:

1. What happened?
2. What does Richmod already know?
3. Why is the user needed?
4. What exact action/input is required?

Example:

~~~text
🟡 Perlu keputusan

Indomaret · Rp57.500 · 23 Sep

Kategori belum cukup jelas.

[Makanan & Minuman]
[Belanja Rumah]
[Lainnya]
[Abaikan]

Lihat detail
~~~

Do not show full forms by default.

Do not expose raw internal UUIDs.

Do not dump source email bodies or sensitive raw document data unless necessary
and explicitly safe for the linked Telegram recipient.

---

# 14. Cross-surface consistency

The same review must not ask different questions on different surfaces.

If Web says:

~~~text
missing_facts = ["transaction_at"]
~~~

Telegram cannot ask for category.

If Telegram resolves category but date remains missing:

- Web must immediately reflect date-only residual;
- Telegram must continue with date-only residual;
- canonical review stays open.

ReviewDecision may evolve as facts are resolved, but the stored canonical state
must remain the source of truth.

---

# 15. Metrics

## 15.1 Telegram Actionable Review Coverage (TARC)

For reviews created while an eligible Telegram recipient exists:

~~~text
reviews with an actionable Telegram projection
/
eligible reviews
~~~

Target after rollout:

~~~text
100%
~~~

Legacy historical reviews may be reported separately.

## 15.2 Web Escape Rate

~~~text
Telegram review interactions that require Web to complete the ordinary blocker
/
Telegram review interactions
~~~

Target:

~~~text
0%
~~~

A user voluntarily tapping View details does not count as an escape.

## 15.3 Completion surface

Track review resolution source:

- TELEGRAM;
- WEB;
- SYSTEM.

This is descriptive product telemetry, not authority.

## 15.4 Cross-surface stale action rate

Track callbacks/replies attempted after review was resolved elsewhere.

The product should answer safely; canonical duplicate mutations target zero.

## 15.5 Review interaction turns

Measure human turns from first actionable review delivery to canonical
resolution.

Use with RHICE; do not optimize turns by hiding genuine missing facts.

## 15.6 Delivery health

Track:

- Telegram eligible reviews;
- projection created;
- send success/failure;
- send retries;
- bind success;
- resolution latency.

Do not store raw financial evidence in metrics.

---

# 16. Current audited gaps at baseline

Baseline main: 1f1e7e6a74c4b7bc1fee15e603717edddf707577

## G1 — universal backlog, non-universal adapter

review_item supports transaction, proposal, source, document, wealth,
financial-email, and cycle subjects.

review_request remains historically transaction-oriented.

## G2 — transaction-only enqueue API

Current EnqueueReviewRequest accepts transactionID and creates both review_item
and review_request rather than projecting an arbitrary existing review_item.

## G3 — transaction-only bound processor

Current processBoundReview joins review_request directly to transaction.

It cannot be the universal resolver for proposal/source/document/wealth/cycle
reviews.

## G4 — explicit Web redirects

Current POSSIBLE_DUPLICATE review messaging tells the user to open Review Inbox.

Some category-only review branches also direct the user to Web.

## G5 — default renderer assumes category

Current reviewInitialState defaults to AWAITING_CATEGORY.

A universal renderer must derive interaction from ReviewDecision, not a generic
review-type fallback.

## G6 — duplicated channel mutation logic

Telegram review flows contain canonical transaction mutation SQL separate from
Web review handlers.

This is a long-term drift risk.

## G7 — cross-surface projection sync is incomplete

A canonical Web resolution updates review/request state, but universal edit/
disable behavior for all Telegram projection types is not guaranteed.

---

# 17. Rollout strategy

Recommended stages:

~~~text
foundation
-> transaction parity
-> proposal/source/document parity
-> Wealth/financial-email/cycle parity
-> cross-surface sync
-> telemetry + universal coverage gate
~~~

Do not ship a "universal" claim while current review types still silently redirect
to Web.

Feature switches may gate Telegram projection generation by review family during
rollout.

A switch may disable delivery.

It must not disable the canonical review itself.

---

# 18. Acceptance scenarios

## A — category-only transaction

Given active Telegram identity and AMBIGUOUS_CATEGORY:

- Telegram message is actionable;
- category can be selected in Telegram;
- Web is not required;
- same review_item resolves;
- RHICE = 1.

## B — missing transaction date

- Telegram asks only date;
- reply binds exactly;
- shared resolver validates date;
- review resolves;
- no category question.

## C — compound date + category

- Telegram collects exactly two residuals;
- partial progress updates ReviewDecision/resolution state;
- review remains open until both close;
- RHICE = 2.

## D — possible duplicate

- candidate summary appears in Telegram;
- Merge / Record New / Ignore are actionable;
- no mandatory Web redirect;
- candidate is revalidated under lock before mutation.

## E — transfer classification + Wealth Account

- Telegram classifies transfer;
- if asset purchase requires Wealth Account, chooser follows;
- same canonical resolver as Web;
- no duplicate transaction.

## F — first salary

- Primary Salary / Ordinary Income / Ignore available in Telegram;
- missing date is requested only when actually missing;
- household policy remains human-owned.

## G — financial email partial resolution

- known entity displayed;
- only unresolved entity selectable;
- Telegram completion updates Web immediately.

## H — Wealth observation

- Set Wealth Account or Prepare Snapshot available in Telegram;
- no Web requirement for the ordinary blocker.

## I — cycle residual

- all allowed policy choices available;
- user can complete decision without Web.

## J — resolve on Web while Telegram button is open

- Web resolution wins;
- Telegram callback returns already-resolved state;
- no duplicate mutation;
- Telegram message becomes non-actionable/resolved.

## K — two Telegram recipients click

- first valid resolution wins;
- second receives stale/resolved response;
- one canonical audit resolution.

## L — Telegram outage

- review_item still exists;
- Web remains usable;
- delivery retry is observable;
- no financial state is guessed or lost.

---

# 19. Non-goals

This sprint does not:

- replace Telegram chat with a menu-only bot;
- remove Web Review Inbox;
- make Telegram canonical storage;
- infer human policy automatically;
- weaken PRD #144 intelligence routing;
- add new AI calls merely to render reviews;
- use an LLM to choose canonical review actions already represented by
  ReviewDecision;
- redesign unrelated dashboard pages;
- turn delivery failure into financial review failure;
- expose raw canonical IDs to the model.

---

# 20. Definition of Done

## Canonical domain

- [ ] review_item remains the one review backlog;
- [ ] Web and Telegram use one canonical resolution service;
- [ ] no channel-specific resolver can bypass ReviewDecision residual guards;
- [ ] first valid resolution is atomic/idempotent.

## Telegram coverage

- [ ] every currently producible review type has a Telegram interaction mapping;
- [ ] no current review type defaults to "open Web" for its ordinary blocker;
- [ ] category/date/compound reviews are actionable;
- [ ] duplicate/conflict reviews are actionable;
- [ ] transfer reviews are actionable;
- [ ] payslip reviews are actionable;
- [ ] bank/document reviews are actionable;
- [ ] financial-email reviews are actionable;
- [ ] Wealth reviews are actionable;
- [ ] cycle residual reviews are actionable.

## Conversation safety

- [ ] exact reply/callback binding preserved;
- [ ] implicit open review cannot swallow unrelated chat;
- [ ] many open reviews do not disable normal conversation;
- [ ] stale callbacks cannot mutate.

## Projection lifecycle

- [ ] eligible active Telegram review creates projection;
- [ ] delivery failure does not remove canonical review;
- [ ] Web resolution disables/updates Telegram projection;
- [ ] Telegram resolution removes Web item;
- [ ] multiple recipients share one canonical resolution.

## UX

- [ ] Telegram shows known facts + reason + exact decision;
- [ ] required controls correspond only to unresolved facts;
- [ ] full Web details are optional, not mandatory;
- [ ] raw UUIDs are never presented as user-facing choices.

## Metrics

- [ ] TARC measurable;
- [ ] Web Escape Rate measurable;
- [ ] completion surface measurable;
- [ ] stale action attempts measurable;
- [ ] delivery success/retry measurable;
- [ ] RHICE semantics remain unchanged.

## Regression

- [ ] current PRD #144 model-call ordering remains unchanged;
- [ ] ReviewDecision parity tests run against Web and Telegram;
- [ ] review type coverage test is exhaustive;
- [ ] adding a new review type without Telegram capability fails an explicit
      contract test;
- [ ] all relevant integration suites pass.

---

# 21. Final product rule

If Richmod has already decided that a human is genuinely required, ask:

~~~text
Can this person finish the decision in the surface they are already using?
~~~

For an active Telegram household, the default answer must be:

~~~text
yes
~~~

The user may choose Web for richer inspection.

Richmod must not require Web merely because the Telegram projection was left
incomplete.
