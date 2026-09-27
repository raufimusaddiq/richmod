# ADR-048 — Validation Consequences, Residual Fidelity, and Domain Continuity

## Status

Accepted — 2026-09-27.

## Context

Many current validators return a boolean/error that is sufficient for local code
but too lossy for product behavior.

Different failures then collapse into the same consequence:

```text
validator failed
→ NEEDS_REVIEW
→ generic review reason
→ known facts may be requested again
```

The problem is not that deterministic validation exists.

The problem is that the validator result does not say what the failure means for
already accepted facts or for the domain lifecycle.

## Decision

Validation that affects product routing must map to a typed consequence.

Minimum vocabulary:

```text
HARD_CANONICAL_INVARIANT
REPRESENTATION_INVALID
INDEPENDENT_EVIDENCE_CONFLICT
BOUNDED_RESIDUAL
QUALITY_SIGNAL
HUMAN_POLICY
CANONICAL_AMBIGUITY
```

This may be implemented as small enums/typed errors/results.

Do not build a generic validation engine or rules DSL.

## Consequence semantics

### HARD_CANONICAL_INVARIANT

Examples:

- invalid currency;
- negative/zero canonical transaction amount where positive is required;
- foreign household ID;
- inactive category/account;
- incompatible transfer relationship.

Action:

- reject, repair, or ask for the exact impossible field;
- never guess.

### REPRESENTATION_INVALID

The source may have meaningful evidence but the current schema/type cannot
represent it.

Example:

- screenshot amount is not visible but schema requires a digit string.

Action:

- preserve all other facts;
- repair representation or mark the field missing;
- do not convert to generic document failure.

### INDEPENDENT_EVIDENCE_CONFLICT

New source evidence materially disagrees with an accepted fact.

Action:

- preserve both provenance paths;
- bounded verifier or exact conflict review;
- no unrelated re-ask.

### BOUNDED_RESIDUAL

One semantic dimension remains unresolved inside a Go-owned candidate set.

Action:

- one Jev decision where appropriate;
- then one exact human choice if still undecided.

### QUALITY_SIGNAL

A consistency/confidence warning that does not itself prove an accepted fact
wrong.

Examples:

- receipt line arithmetic differs from authoritative total;
- payslip breakdown cannot be reconciled while printed net pay remains clear.

Action:

- preserve authoritative facts;
- block only the capability the quality signal actually makes unsafe;
- do not invent unrelated missing facts.

### HUMAN_POLICY

The source cannot decide household intent.

Examples:

- first primary salary source;
- residual allocation.

Action:

- ask the policy choice only.

### CANONICAL_AMBIGUITY

Multiple canonical candidates remain safe/plausible.

Examples:

- duplicate candidates;
- account/wealth mapping ambiguity.

Action:

- bounded human selection.

## Residual derivation

Review residuals derive from consequences, not from a generic review-type
fallback.

If a receipt has:

```text
total known
date known
category known
arithmetic quality signal
```

then `category` is not a residual.

## Domain continuity

A domain candidate survives review.

The domain candidate/finalizer owns domain-specific side effects.

Review may mutate accepted facts through explicit user correction, then calls the
same finalizer.

## Finalization parity

For equivalent accepted facts:

```text
autonomous finalizer result
==
human-resolved finalizer result
```

except the fields the human explicitly changed.

Tests must compare side effects, not only transaction status.

## Payslip requirement

The generic `MANUAL_CORRECTION` path must not be the terminal architecture for
an arithmetic-blocked payslip.

A reviewed payslip must remain bound to salary-domain state and reach the salary
finalizer after resolution.

Fixing one period regex is not finalization parity.

## Representation requirement

Unknown/missing values must be representable explicitly at extraction/domain
boundaries.

Sentinel values that later fail canonical validation are prohibited when the
source can genuinely omit the field.

## Review system interaction

ADR-046 remains unchanged.

SAVR feeds UIR a more faithful ReviewDecision.

UIR continues to own:

- projection;
- surface parity;
- stale handling;
- first-write-wins.

SAVR owns:

- what facts are known;
- what validation consequence exists;
- what exact residual remains;
- which domain finalizer runs.

## Consequences

Positive:

- review reasons become truthful;
- known facts survive quality/representation problems;
- domain side effects stay consistent;
- human interaction maps to real uncertainty.

Cost:

- existing boolean/error validators require classification;
- some review reasons need narrower decisions;
- domain finalizers may need extraction from current auto-only paths.

## Non-goals

No generic workflow framework.

No universal validation engine.

No schema migration unless a source/domain cannot represent the required state
with existing structures.

## Receipt implementation (SAVR-02/06)

ReviewDecision now carries an optional typed `validationConsequence` and
`affectedFacts` pair. For receipt line/total mismatch, Go records
`QUALITY_SIGNAL` targeting `receipt_arithmetic`. With category and date known,
the existing `RECEIPT_MISMATCH` reason asks no missing semantic fact; the
household may explicitly accept the printed total or ignore the evidence.
Canonical amount/category/date validation remains in the shared transaction
review finalizer. Candidate duplicates still take priority.
