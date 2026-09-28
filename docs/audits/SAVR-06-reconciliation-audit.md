# SAVR-06 Reconciliation Audit — North-Star Checkpoint

**Status:** post-merge product audit  
**Baseline:** `main@904ff1acf6bdd8954d49728d7cc2623b5eee8d45`  
**Date:** 2026-09-28  
**Scope:** SAVR-01 through SAVR-06 implementation compared with PRD #144,
BDR-004, ADR-047, ADR-048, UIR's frozen review contract, and the Richmod north
star.

---

# 1. Audit question

This audit does not ask whether every review reason is precise.

It asks:

> **Does Richmod already know enough to keep household financial state correct
> without involving the user?**

The north-star test is:

```text
understand once
-> minimum sufficient intelligence
-> preserve accepted facts
-> canonicalize when material semantics are sufficient
-> human only for irreducible material residual/policy/conflict
```

A change is drift when it makes a non-material uncertainty, generic confidence,
redundant hint, machine failure, or weak candidate increase RHICE.

---

# 2. Non-negotiable reconciliation rules

## R1 — Only material uncertainty may increase RHICE

An exact residual is not enough. It must be capable of changing the attempted
canonical financial outcome, satisfying a hard canonical invariant, resolving a
material evidence conflict, or obtaining irreducible household policy.

## R2 — Semantic sufficiency ends semantic review

Valid intelligence paths are:

```text
Go only
Jev only
LLM only
LLM -> Jev for one named material bounded residual / independent evidence check
USER -> Go
```

No path requires LLM and Jev by default.

## R3 — Go owns canonical safety, not another semantic vote

Keep authorization, canonical IDs, positive/representable money, supported
currency, household isolation, duplicate/reconciliation safety, compatibility,
concurrency, and mutation guards.

Do not add generic confidence or ontology votes after the selected intelligence
path has already resolved the material semantics.

## R4 — System-derived policy facts can be canonical

For bank email, no printed transaction time intentionally means:

```text
transaction_at = source_event.received_at
transaction_at_source = EMAIL_RECEIVED_AT
RHICE += 0
```

This is a healthy policy and MUST NOT be "fixed" into a missing-time review.

## R5 — Machine inability is not human uncertainty

Malformed schema/model output, provider outage, parser limitation, or
representation failure stays retry/repair/infrastructure state unless a concrete
material user-suppliable fact remains.

---

# 3. Sprint reconciliation status

| Sprint | Audit status | Evidence / remaining debt |
| --- | --- | --- |
| SAVR-01 | PARTIAL | phase dimension arrays normalized and known-fact re-ask metric exists; validator-induced review, residual-fidelity ground truth, and semantic re-decision coverage remain explicitly incomplete |
| SAVR-02 | PARTIAL | consequence vocabulary exists and receipt arithmetic uses `QUALITY_SIGNAL`; adoption across every active validator is not complete |
| SAVR-03 | DELIVERED FOR SCOPED REPRESENTATION | screenshot missing amount is representable; payslip period/gross/other-components representation improved without sentinel semantics |
| SAVR-04 | DELIVERED | receipt, screenshot, bank email, and Telegram share exact household merchant/category memory |
| SAVR-05 | MATERIALLY ALIGNED, NOT CLOSED | explicit batch confirmation no longer replays Jev; current `semanticDecisionForRecord` accepts complete generative results directly and uses Jev only for named residual category. Remaining source/correction parity belongs to SAVR-08 |
| SAVR-06 | MERGED, RECONCILIATION REQUIRED | bank mechanism materiality fixed in PR #202 and provider residuals are more exact; cross-source confidence/hint/machine-failure gates below remain north-star debt |

SAVR-07 must remain paused until the reconciliation gate is accepted and its
confirmed implementation corrections are merged.

---

# 4. Healthy boundaries — preserve them

## H1 — Bank email timestamp fallback

Current `applyEmailReceivedTimeFallback` sets
`transaction_at = received_at`, marks `EMAIL_RECEIVED_AT`, removes the time
residual, and does not ask the household for a time.

**Classification:** HEALTHY / KEEP.

## H2 — Bank payment mechanism is non-material

PR #202 removed `channel_supported` as a blocking QR/debit/merchant-method
predicate and replaced it with a material semantic grounding question.

**Classification:** HEALTHY DIRECTION / KEEP.

Do not regress to asking the household which payment mechanism produced an
otherwise clear expense.

## H3 — Telegram single-record minimum-sufficient routing

Current `semanticDecisionForRecord` accepts a complete generative transaction
directly and asks Jev only for a genuine residual category. It does not route a
clear LLM result through the older whole-transaction decision bundle.

**Classification:** HEALTHY / KEEP.

## H4 — Explicit batch user authority

A shown-and-confirmed batch does not get another Jev semantic vote.

**Classification:** HEALTHY / KEEP.

## H5 — Shared exact merchant memory

Known household merchant/category state is reused before model/human escalation.

**Classification:** HEALTHY / KEEP.

---

# 5. Confirmed post-SAVR-06 drift

## D1 — Receipt confidence can manufacture a fake category review

Current receipt auto-confirm still requires:

```text
value.Confidence >= 0.90
```

When amount/date/category are accepted, no duplicate exists, and arithmetic is
not blocking, missing this generic threshold falls through to
`receiptReviewReason`, whose default is `AMBIGUOUS_CATEGORY`.

The category may already be known.

**Classification:** IMPLEMENTATION_DRIFT.

**Required product correction:** confidence may remain telemetry/quality input,
but confidence-only failure cannot create a category residual or human task. If
there is a real material dimension behind low confidence, name that dimension;
otherwise canonicalize.

**Resolution (branch `fix/savr-post06-reconciliation`):** `persistReceipt`
links strong duplicate evidence or confirms a complete new receipt irrespective
of generic confidence. Category, date, arithmetic, and material duplicate
guards remain.

## D2 — Screenshot has the same confidence-only fake residual

`validatedScreenshotRow.autoConfirmable()` requires
`row.Value.Confidence >= .90`.

A row with known amount/date/category, no category conflict, and no material
duplicate can therefore miss auto-confirm solely on confidence and then fall
through to `AMBIGUOUS_CATEGORY`.

**Classification:** IMPLEMENTATION_DRIFT.

**Required product correction:** same as receipt. Generic confidence is not a
human fact.

**Resolution (branch `fix/savr-post06-reconciliation`):** screenshot rows with
accepted material facts confirm despite low generic confidence; strong
duplicate evidence still links instead of writing a second transaction.

## D3 — Provider email can forget a canonical selected account because a prose hint is absent

`planCash` currently checks `FundingAccountHint == nil` before it applies
`selectedAccount`.

That means a previously resolved canonical account can still be blocked by the
absence of a redundant source hint.

**Classification:** IMPLEMENTATION_DRIFT.

**Required product correction:**

```text
selected/resolved canonical account exists
-> use it
-> missing prose hint is irrelevant

prose hint materially conflicts with canonical account
-> exact conflict
```

**Resolution (branch `fix/savr-post06-reconciliation`):** a selected active
household account survives the missing hint; a resolvable contradictory hint
opens an account-specific evidence-conflict review. Once the household resolves
that conflict, replay accepts the explicit decision without re-asking.

## D4 — Provider `evidence_sufficient` is still a bundled gate

The bounded predicate asks whether amount/value, time, **and account hints** are
all supported and exposes one `evidence_support` residual.

This can make a non-material hint subpart block an otherwise complete financial
movement.

**Classification:** SPEC_AMBIGUITY + IMPLEMENTATION_DEBT.

**Required product correction:** verify only dimensions material to the
attempted canonical outcome. Do not add another model call merely to split the
bundle; derive or ask the minimum bounded material claim set.

**Resolution (branch `fix/savr-post06-reconciliation`):** the
`evidence_sufficient` predicate now checks the material value/amount and, for a
cash movement, the transaction time. It no longer requires the prose account
hint; account canonicalization belongs to Go. Claim-key coverage is unchanged,
so no extra intelligence pass was added.

## D5 — Bank schema-invalid state can become pseudo human work

A bank `SchemaError` persists `INVALID / NEEDS_REVIEW` at the source event,
but the failure can be malformed machine output rather than missing household
knowledge.

**Classification:** IMPLEMENTATION_DRIFT.

**Required product correction:** classify machine/schema/provider failure before
human review. Create ReviewDecision only when a specific material fact/policy can
actually be supplied by the household.

**Resolution (branch `fix/savr-post06-reconciliation`):** bank schema output,
provider verification outage, and unrepairable document output remain failed
machine/job state, without generating human work. Accepted source evidence is
retained for operator repair.

---

# 6. Re-audit candidates before code changes

These are real risk areas, but the product contract should be applied before
choosing a patch.

## A1 — Duplicate candidate materiality

Receipt matching returns every same-amount, same-direction transaction inside a
72-hour window, including low-scoring different-merchant rows. Caller logic can
treat candidate existence as `POSSIBLE_DUPLICATE`.

Duplicate safety is valid and must stay. However:

> candidate existence != canonical ambiguity

Re-audit and define minimum material plausibility before increasing RHICE. Do not
simply delete duplicate guards.

**Classification:** REAL_PRODUCT_GAP / NEEDS MATERIALITY CONTRACT.

**Resolution (branch `fix/savr-post06-reconciliation`):** receipt/screenshot
same-amount, same-type candidates remain plausible (and still block) only with an exact
merchant match within 72 hours or any same-amount transaction within one
hour. A different-merchant candidate 12–72 hours apart is a query hit, not
human ambiguity; duplicate safety for the plausible cases is unchanged.

## A2 — Review projection remains source-origin-coupled

Bank and provider-email projection helpers currently derive Telegram delivery
only from Telegram-origin source payloads. Email-origin reviews can therefore
remain Inbox-only even when the household has an eligible active Telegram
recipient.

This is not a SAVR semantic redesign. It is an adjacent UIR production-contract
regression:

```text
canonical human review exists
+ household has eligible Telegram recipient
-> project to Telegram
```

**Classification:** ADJACENT_UIR_DEFECT.

Track/fix separately; do not change SAVR financial authority to work around it.

---

# 7. SAVR-07 requirements after reconciliation

Payslip work must not inherit the hard-gate patterns above.

In addition to domain continuity/finalizer parity:

- generic `confidence >= 0.95` cannot be the sole reason for human review once
  material salary facts are accepted;
- arithmetic quality may block only the capability it actually makes unsafe;
- actual receipt/pay date and salary-source policy remain domain facts;
- human-resolved and autonomous salary candidates use the same finalizer;
- the user is not asked to explain a payslip simply because the machine emitted a
  quality score below a threshold.

---

# 8. SAVR-08 through SAVR-10

## SAVR-08

Re-run every active source boundary with these additional columns:

```text
attempted canonical outcome
material dimensions
non-material dimensions
minimum sufficient intelligence path
system-derived policy facts
why each AI call exists
why each human input exists
```

Any exact-but-non-material review is a failure.

## SAVR-09

Corpus/canary must measure:

- RHICE;
- known-fact re-ask;
- validator-induced review;
- residual fidelity;
- semantic re-decision;
- calls/event;
- post-auto-confirm material correction.

Add explicit regression cases for:

- receipt with complete facts and confidence below old threshold;
- screenshot with complete facts and confidence below old threshold;
- provider movement with canonical selected account but no redundant source hint;
- bank email with no transaction time using `EMAIL_RECEIVED_AT`;
- non-material QR/debit mechanism uncertainty;
- machine/schema failure that does not create fake human work;
- duplicate query hit that is not materially plausible.

## SAVR-10

Remove obsolete semantic gates only after the corpus proves the replacement path.

Do not close SAVR merely because review reasons are precise.

Closure requires:

> **No non-material uncertainty, redundant intelligence pass, or machine-only
> failure can unnecessarily increase human effort.**

---

# 9. Implementation gate before resuming

Before starting SAVR-07:

1. merge this docs reconciliation;
2. perform the bounded post-SAVR-06 implementation corrections above in FIFO
   review cycles;
3. rerun the updated drift guard;
4. verify the exact latest main;
5. only then resume SAVR-07.

This checkpoint does not introduce a new AI layer, review subsystem, rules DSL,
or workflow engine.
