# SAVR-00 — Semantic Authority Boundary Audit

> **Status note (2026-10-02):** `executeNativeTool`, `processAssistantIntent`, and
> `resolveTransactionDecision` named in this document were removed as unreachable
> for production traffic; see the addendum in
> `docs/audits/P0_YAGNI_TECH_DEBT_CLEANUP.md`. The live typed-text path is
> `ProcessAgent`.

**Status:** baseline audit complete for architecture PR  
**Audited main:** `ffb29a15f13bef32fa8da0d840be75e3501fd928`  
**Date:** 2026-09-27

This audit is the starting evidence for SAVR.

Codex must refresh each row against latest main before implementing its task.

---

# 1. Executive summary

UIR is now a sufficiently stable review substrate. The remaining material debt
is upstream of review: accepted facts can still be reinterpreted, collapsed by
validation, or stripped of domain identity before ReviewDecision is created.

Confirmed latest-main findings:

| ID | Boundary | Finding | SAVR pillar | Target |
| --- | --- | --- | --- | --- |
| S00-01 | payslip extraction → validation | period/parser + arithmetic contract can reject otherwise useful facts | representation / consequence | SAVR-03/SAVR-07 |
| S00-02 | payslip validation → review | arithmetic-blocked existing-primary path becomes generic `MANUAL_CORRECTION` transaction review | domain continuity | SAVR-07 |
| S00-03 | screenshot extraction schema | amount required as digits; unknown amount cannot be represented, `"0"` is schema-valid then validator-invalid | representation | SAVR-03 |
| S00-04 | screenshot category | learned `merchant_alias` is not consulted before Jev | known-fact reuse | SAVR-04 |
| S00-05 | receipt validation → review | arithmetic mismatch with known date/category falls through to `AMBIGUOUS_CATEGORY` | residual fidelity | SAVR-06 |
| S00-06 | bank Jev verification → review | any unsupported predicate collapses to generic `transaction_semantics`; known transaction_at omitted from partial decision | residual fidelity | SAVR-06 |
| S00-07 | bank negative verification → observability | unsupported verdict returns before `persistEvidenceVerification` | provenance | SAVR-01/SAVR-06 |
| S00-08 | provider email plan | multiple distinct blockers collapse to `TRANSFER_CLASSIFICATION` | residual fidelity | SAVR-06 |
| S00-09 | Telegram staged batch → confirm | explicit user confirmation re-runs semantic decision for every item | user authority / re-decision | SAVR-05 |
| S00-10 | intelligence telemetry | Jev recorder passes nil residual dimensions into an explicit NOT NULL column | observability | SAVR-01 |

Reverify before claiming a defect:

- natural Indonesian date handling across the generative single-record path;
- exact semantic ownership for every field in `semanticDecisionForRecord`;
- disabled unified document interpretation contract.

---

# 2. Payslip

## Confirmed

`apps/worker/internal/document/payslip.go` still uses a constrained payslip
representation and deterministic validation that includes:

- unsigned money strings for lines;
- strict period parsing;
- arithmetic based on gross/allowances/deductions/net relationships;
- one restricted repair followed by the same validator.

The period range parser remains structurally brittle for real forms such as:

`September 2026 (01/09/26 - 30/09/26)`

Important correction preserved from prior audit:

- caption-derived pay date is applied **before** validation;
- do not reintroduce the old claim that caption fallback runs after validation.

## Domain continuity defect

Current persistence distinguishes:

```text
reviewWithoutTransaction := !hasPrimary || value.PayDate == nil
```

When a household already has a primary salary and a pay date exists, an
arithmetic-blocked payslip can create a generic NEEDS_REVIEW transaction and
later `MANUAL_CORRECTION`.

That path is outside the proposal-based payslip resolver.

UIR improved the proposal review path:
`reviewdomain.ResolvePayslipProposal` can execute salary event/finalization
behavior.

But the arithmetic-blocked generic path still loses salary-domain continuity.

## Required acceptance case

Given a payslip with accepted:

- net pay;
- pay date;
- employer;
- payroll period;

and a breakdown that fails arithmetic reconciliation:

- accepted facts remain known;
- review, if required, stays salary-domain;
- no amount/date re-ask unless those exact facts are missing/conflicting;
- human resolution reaches the same salary finalizer as autonomous resolution.

---

# 3. Screenshot

## Representation defect

`screenshotSchema` requires:

```text
amount: string, pattern ^[0-9]+$
```

for every row.

The deterministic validator later requires positive whole money.

Therefore genuine "amount not visible" cannot be represented. A model can emit
`"0"`, which satisfies the schema but fails the validator.

Target:

- extraction may represent amount missing;
- canonical transaction still requires positive amount;
- only amount becomes residual.

## Household knowledge defect

`resolveRowCategories` sends unresolved expense rows to Jev but does not first
reuse exact `merchant_alias` knowledge.

Other pipelines already have source-specific versions of this behavior:

- receipt `receiptCategory`;
- bank `loadMerchantMemory`;
- Telegram `exactMerchantCategory`.

This is evidence for one shared deterministic household-knowledge resolver, not
for copying another SQL query into screenshot code.

---

# 4. Receipt

`validateReceipt` calculates arithmetic consistency when subtotal data is
available.

Auto-confirm requires arithmetic consistency when that arithmetic is available.

That conservative gate is not itself the defect.

The defect is the consequence:

`receiptReviewReason(... categoryKnown=true, dateKnown=true)`

falls through to:

`AMBIGUOUS_CATEGORY`.

Thus arithmetic quality can be presented as category uncertainty.

Target:

- arithmetic mismatch classified as a quality/conflict consequence;
- total/date/category remain known;
- review asks only a real residual, if one exists.

---

# 5. Bank email

The bank evidence verifier asks separate bounded predicates:

- transaction observed;
- amount supported;
- direction supported;
- channel supported;
- material ambiguity.

This independent evidence check remains valid under ADR-045.

## Residual collapse

Processor behavior currently does:

```text
verified && !verification.supported()
→ UNKNOWN_BANK_TEMPLATE
→ missingFacts = ["transaction_semantics"]
```

One failed predicate therefore loses specificity.

`partialDecision` preserves amount, direction, channel, merchant, but does not
currently preserve known `transaction_at`.

## Negative-verdict provenance gap

`persistEvidenceVerification` is called only after the unsupported-verification
early return.

Therefore the exact bounded verdict that caused review is not persisted by that
path.

Target:

- persist the predicate outcome that caused review;
- derive exact residual/conflict;
- preserve unrelated accepted facts.

---

# 6. Financial-provider email

`planCash` contains a good exact-residual pattern:

```text
FINANCIAL_EMAIL_RESOLUTION
+ resolutionGaps(account, wealth)
```

This is the pattern to preserve.

The same planner also maps multiple distinct blockers to
`TRANSFER_CLASSIFICATION`, including structural/verification/time/purpose/
compatibility failures.

Target:

- classify consequence before review;
- do not convert a known validation reason into generic transfer ambiguity.

---

# 7. Telegram

## Explicit batch confirmation

`agentFinalizePendingBatch(confirm=true)`:

1. loads the already staged items;
2. validates amount/category IDs;
3. then calls `resolveTransactionDecision` for every item;
4. cancels/routes review if semantic decision is not allowed.

This means an explicit human confirmation can be semantically replayed.

Target:

- keep structural/canonical validation;
- do not ask Jev to approve already confirmed semantic dimensions;
- if a staged item still had an unresolved dimension, it should not have been
  presented as fully confirmable in the first place.

## Single-record path

`agentRecordTransaction` calls `semanticDecisionForRecord` after generative
native extraction.

Do not blindly remove it.

SAVR-05 must audit each dimension:

- which was user-stated;
- which was generatively extracted;
- which is bounded residual;
- which Jev question is independent evidence support;
- which is redundant replay.

---

# 8. Intelligence telemetry

Migration 00065 defines `residual_dimensions` as NOT NULL with an empty-array
default.

Worker `recordPhase` explicitly supplies `phase.ResidualDimensions`.

The Jev adapter `recordIntelligencePhase` maps `systemone.Metric` to
`gateway.CallMetric` without setting residual dimensions.

Because the insert explicitly supplies the column, the database default cannot
repair an explicit NULL.

This is a confirmed static observability defect on the audited baseline.

Target:

- normalize nil to an empty array or otherwise satisfy the existing telemetry
  contract;
- add a test;
- do not redesign the telemetry schema merely for this defect.

---

# 9. Healthy boundaries to preserve

SAVR must not regress these:

- UIR shared review-domain operations;
- ReviewDecision as canonical human residual contract;
- exact household-scoped entity validation;
- duplicate/reconciliation locking;
- bank evidence verification as an independent evidence purpose;
- financial-email `resolutionGaps` behavior;
- first-valid-resolution semantics;
- PRD #144 call-order rules.

---

# 10. Required implementation evidence

Every source migration PR must state:

```text
semantic dimensions touched
owner before
owner after
canonical guards retained
validators removed/reclassified
known facts preserved
residual before/after
model calls before/after
human inputs before/after
domain finalizer
regression corpus cases
```

A PR that fixes the symptom without answering these is not SAVR-complete.
