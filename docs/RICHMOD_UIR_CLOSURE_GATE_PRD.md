# RICHMOD — UIR CLOSURE GATE

## Product Requirements Document for Codex /goals

**Status:** APPROVED PRODUCT DIRECTION / PRE-SAVR CLOSURE GATE  
**Repository:** raufimusaddiq/richmod  
**Audited baseline:** `main@fabe7368c76dff2032685e23ef037bcefe36dcef`  
**Date:** 2026-09-27  
**Depends on:** Universal Review Interaction PRD, BDR-002, ADR-046, PRD #144  
**Next product sprint after this gate:** SAVR — Semantic Authority & Validation Reconciliation

---

# 0. Why this exists

UIR-00..UIR-10 are delivered, but the post-delivery product audit found a
small set of contract gaps that make the sprint unsafe to declare product-closed.

The objective is **not** to reopen UIR as another large feature sprint.

The objective is:

> close the remaining Universal Review Interaction contract gaps with the
> smallest durable changes, freeze the UIR boundary, then start SAVR.

This gate exists because SAVR depends on a trustworthy review layer:

```text
accepted semantic facts
        ↓
ReviewDecision
        ↓
review_item
        ↓
shared review resolver
        ↓
Telegram / Web projections
```

SAVR must not be built on top of review paths that still require hidden Web
escapes, duplicate canonical mutations, or misleading review telemetry.

---

# 1. Product contract being closed

The existing UIR contract remains authoritative:

- `review_item` is the canonical human-decision backlog;
- `ReviewDecision` is the canonical statement of known / proposed / unresolved
  facts;
- Telegram is the fastest completion surface;
- Web is the richest inspection surface;
- the ordinary blocker must be completable from Telegram for an eligible
  Telegram household;
- Web and Telegram must resolve through the same canonical Go rules;
- the first valid resolution wins;
- optional "View details" navigation is allowed, but mandatory Web escape is not.

This closure gate does **not** redefine those decisions.

---

# 2. YAGNI rules

Every implementation choice in this gate MUST prefer the smallest change that
satisfies the existing product contract.

## Required restraint

Do:

- reuse `review_item`, `ReviewDecision`, `review_request`,
  `review_conversation`, `apps/reviewdomain`, existing Telegram agent flows,
  and existing Admin Review Ops;
- move duplicated canonical mutation into an existing domain operation or one
  narrowly scoped operation;
- delete or consolidate duplicate logic where possible;
- derive truth from existing canonical/audit state before adding storage;
- add tests at the exact boundary that regressed.

Do **not**:

- create a new review subsystem;
- create a generic workflow engine;
- create a new event bus;
- add Redis/Kafka/queues beyond existing jobs;
- add a new AI/model call for deterministic review actions;
- redesign the Admin UI;
- build a full Telegram Wealth product;
- redesign document extraction, payslip arithmetic, merchant learning, or model
  routing;
- add a schema migration unless an existing invariant cannot be represented
  safely without it;
- refactor unrelated code for cleanliness.

If an implementation requires a large new abstraction to close one gap, stop and
re-evaluate the design.

---

# 3. Explicitly out of scope — belongs to SAVR

The following are known but MUST NOT be pulled into this closure sprint:

- payslip period / payroll arithmetic representation;
- document extraction schema adequacy;
- screenshot amount unavailable vs `"0"`;
- cross-channel merchant_alias reuse;
- LLM/Jev accepted-fact preservation;
- natural-language date semantic ownership;
- Jev semantic re-decision after explicit user confirmation;
- validator taxonomy / consequence redesign;
- generic review reason collapse caused by semantic validation;
- known-fact re-ask redesign;
- unified document interpretation enablement.

These are SAVR inputs, not UIR closure work.

---

# 4. Confirmed closure gaps

## 4.1 Mandatory Web escape: cycle residual `TRANSACTION_MISSING`

Current Telegram behavior still produces an explicit Web requirement for this
action.

Required product behavior:

```text
CYCLE_RESIDUAL_ALLOCATION
user: "ada transaksi yang belum tercatat"
        ↓
use the existing canonical Telegram transaction intake
        ↓
canonical transaction commits
        ↓
cycle basis is recomputed
        ↓
same review closes / refreshes / remains actionable
```

The user must not be sent to Web merely to add the missing transaction.

### YAGNI implementation rule

Do not build a new cycle-specific transaction form.

Reuse the normal Telegram transaction flow. The cycle review may remain open
while the transaction is entered. After the canonical transaction is committed,
recompute/re-enqueue the existing residual reconciliation deterministically.

---

## 4.2 Mandatory Web escape: unlinked bank source

`UNKNOWN_BANK_TEMPLATE` can be completed from Telegram only after the bank
source is linked to a household account. The current unlinked case sends the
user to Review Inbox.

Required product behavior:

- present a bounded household account chooser in Telegram;
- link/revalidate the source account through server-owned IDs;
- continue the existing bank completion path;
- do not build a general bank-settings flow in Telegram.

The account linkage is a prerequisite to the current blocker, so it is part of
the same actionable review path.

---

## 4.3 Expired projection must not force Web

A Telegram request TTL is a projection/conversation concern. It does not expire
the canonical `review_item`.

If the user interacts with an expired Telegram projection and the canonical item
is still open:

- do not resolve or discard the item;
- do not require Web;
- create/reuse a fresh actionable Telegram projection or provide a deterministic
  "review berikutnya" continuation.

No new TTL model is required.

---

## 4.4 Wealth `PREPARE_SNAPSHOT`: classify it correctly, do not expand scope

UIR does not require building the whole Wealth snapshot editor in Telegram.

For this closure:

- `SET_WEALTH_ACCOUNT` and `IGNORE` remain ordinary review completion paths
  and must be fully completable in Telegram;
- `PREPARE_SNAPSHOT` may remain an explicit **voluntary richer-workflow
  navigation** if snapshot authoring itself is a separate Wealth workflow;
- it must not be counted as a mandatory Web escape;
- it must not be presented as though the current review was completed when it was
  not;
- any still-required canonical review after navigation remains visible/actionable.

Do not build snapshot authoring in Telegram as part of this gate.

---

# 5. Shared canonical resolution gaps

ADR-046 requires surface-neutral financial mutation.

Post-delivery audit still found canonical review work implemented separately in
surface adapters.

## 5.1 Transfer reconciliation

The transfer-reconciliation operation that:

- validates the reconciliation case;
- validates a selected candidate;
- confirms/creates the canonical transfer;
- attaches evidence;
- updates financial-email/source state;
- resolves the reconciliation case;
- resolves the canonical review/projection;

must have **one** channel-neutral implementation.

Web, bound Telegram, and Telegram agent adapters may decode different inputs,
but they must call the same operation.

Do not create a universal mega-resolver. Add/move only the transfer
reconciliation operation needed here.

## 5.2 Financial-email review lifecycle

Entity validation already has shared domain logic, but partial binding, ignore,
terminal review completion, source refresh, and replay enqueue still have
surface-owned lifecycle pieces.

Close only the duplicated lifecycle needed for parity:

- partial entity choice;
- ignore;
- final entity resolution;
- canonical review/projection completion;
- source replay transition.

The domain operation owns state transition. Surfaces own UI/binding/audit wording.

---

# 6. Canonical Review Inbox read authority

The product statement is:

> `review_item` is the inbox.

The current Web list still contains a legacy transaction-first read path in
addition to canonical `review_item` reads.

Closure requirement:

1. prove that every current producer requiring human review creates a
   `review_item`;
2. make current active review rendering derive from the canonical item/decision;
3. retain a narrow compatibility fallback only if historical transaction-only
   rows still require it;
4. current producers MUST NOT depend on that fallback.

### YAGNI rule

Do not migrate all historical data just to remove a compatibility read.

Do not redesign the Inbox API shape unless required for correctness.

---

# 7. Review observability must describe reality

Admin Review Ops is part of the UIR product contract. Metrics must not be merely
present; they must mean what their labels say.

## 7.1 Resolution surface

Do not infer TELEGRAM vs WEB from whether the resolving user happens to own a
Telegram identity.

Resolution surface must come from the actual resolution action.

Preferred YAGNI direction:

- standardize/reuse existing `audit_log.actor_type` / canonical resolution audit
  emitted by the resolving surface;
- do not add a new telemetry table if existing audit state can answer the
  question correctly.

A user linked to Telegram who clicks Web must count as WEB.

## 7.2 Web Escape Rate

Web Escape means:

> a Telegram review interaction could not complete its ordinary blocker without
> requiring Web.

It does not mean "any review resolved on Web".

Exclude:

- voluntary View details;
- voluntary richer Wealth workflow navigation;
- a user independently choosing Web despite Telegram being actionable.

## 7.3 Telegram Actionable Review Coverage (TARC)

A delivered Telegram message is not automatically actionable.

A review counts in the numerator only when:

- the canonical review is eligible/open;
- a Telegram projection exists and is delivered/live;
- the review type/action has an implemented Telegram completion lane for its
  current ReviewDecision.

Do not inflate TARC from `telegram_message_id IS NOT NULL` alone.

---

# 8. Structural producer → capability gate

The current "exhaustive" test is backed by a manually maintained list of
producible review types. That is useful but not sufficient to prove producer
coverage.

Required outcome:

> adding a new current review producer without ReviewDecision + Telegram
> completion capability must fail CI by construction.

Use the smallest production-used source of truth.

Acceptable direction:

- one Go review capability/producer registry used by actual projection/decision
  code and by the contract test; or
- another equally small mechanism where producers cannot emit an unregistered
  type.

Compatibility-only schema values remain explicitly marked as such.

Do not add runtime plugin infrastructure or database capability tables.

---

# 9. Definition of Done

UIR closure is complete only when all of the following are true on merged main:

1. no active ordinary Telegram review blocker has a mandatory Web escape;
2. cycle `TRANSACTION_MISSING` can continue through the existing Telegram
   transaction flow;
3. an unlinked bank source can be linked and completed from Telegram;
4. an expired projection can be continued/re-projected without requiring Web;
5. Wealth snapshot navigation is classified as optional richer workflow, not a
   fake completed review or mandatory escape;
6. transfer reconciliation canonical mutation has one domain implementation;
7. financial-email review lifecycle no longer duplicates canonical transition
   semantics across Web and Telegram;
8. current active Inbox work is canonically represented by `review_item`;
9. resolution-surface telemetry records the actual surface;
10. Web Escape Rate follows the definition above;
11. TARC measures actionable completion, not delivery only;
12. a new producer without capability causes a contract test failure;
13. first-valid-write and stale-action behavior remain unchanged;
14. deterministic review actions introduce no new model call;
15. PRD #144 intelligence routing is unchanged;
16. SAVR scope listed in §3 remains untouched.

After these gates pass, UIR is frozen and SAVR may start.

---

# 10. Product acceptance scenarios

## C1 — missing transaction from cycle review

Given an open cycle residual review in Telegram, when the user says a transaction
is missing, Richmod lets them record it through the normal Telegram transaction
flow and then refreshes the cycle residual. No Web requirement is introduced.

## C2 — bank source not linked

Given an UNKNOWN_BANK_TEMPLATE review whose source lacks an account binding,
Telegram shows a bounded active household account chooser. After the user picks
one, the existing bank completion path resumes.

## C3 — expired card

Given an expired Telegram projection and an open canonical review, user
interaction yields a fresh actionable continuation. The canonical review remains
open and no Web requirement is introduced.

## C4 — transfer reconciliation parity

Resolving the same transfer reconciliation fixture from Web and Telegram calls
the same domain operation and produces equivalent canonical transaction,
evidence, reconciliation-case, review, and source-event state.

## C5 — actual completion surface

A Telegram-linked user resolves a review on Web. Admin reports WEB, not TELEGRAM.

## C6 — actionable coverage

A delivered fixture with no completion capability does not increase TARC.

## C7 — producer regression

A test-only new producer/type without registered decision/completion capability
fails the coverage contract.

---

# 11. Handoff to SAVR

Once this gate is merged and verified:

```text
UIR = frozen interaction / resolution substrate
        ↓
SAVR-00 = exhaustive semantic-authority audit
        ↓
SAVR PRD / BDR / ADR / execution
```

Do not continue polishing UIR after closure unless a production defect violates
the frozen contract.
