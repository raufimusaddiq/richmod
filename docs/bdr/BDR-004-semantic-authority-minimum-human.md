# BDR-004 — Semantic Authority Without Repeated Consensus

## Record type

Business Decision Record.

## Status

Accepted product decision — 2026-09-27.

## Decision owner

Product Design.

## Baseline

`main@ffb29a15f13bef32fa8da0d840be75e3501fd928`

## Related documents

- `docs/RICHMOD_SEMANTIC_AUTHORITY_VALIDATION_RECONCILIATION_PRD.md`
- `docs/adr/ADR-047-semantic-fact-ownership.md`
- `docs/adr/ADR-048-validation-consequence-domain-continuity.md`
- `docs/RICHMOD_INTELLIGENCE_ROUTING_MINIMAL_INTERACTION_PRD.md`
- `docs/adr/ADR-045-single-intelligence-pass-routing.md`
- `docs/adr/ADR-039-canonical-review-decision-contract.md`
- `docs/adr/ADR-046-universal-review-interaction-projection.md`

## Business problem

Richmod can already extract, classify, verify, review, and persist financial
events. The remaining user friction often does not come from missing
intelligence.

It comes from **semantic rework**.

A fact is understood once, then a later layer reinterprets it using a narrower
representation or heuristic. The second layer may be deterministic and
well-intentioned, but the product result is still poor:

- known facts disappear;
- the wrong review reason is produced;
- a model is asked again;
- or the user is asked to re-enter a fact.

This is especially costly because the system appears to know the answer and then
behaves as if it forgot it.

## Decision

Richmod will use **single semantic ownership per fact/dimension**.

Once a source-specific acceptance contract accepts a semantic fact, downstream
code may:

- validate canonical safety;
- map it to canonical IDs;
- detect independent conflict;
- preserve provenance;
- or reject an impossible representation.

Downstream code may not silently re-understand the same semantic dimension.

## User authority

An explicit user confirmation or correction is semantic authority unless:

- the value violates a hard canonical invariant;
- the referenced canonical entity is invalid/foreign/inactive;
- a concurrent canonical decision already won;
- new independent evidence creates a material conflict.

Jev is not an approval committee over explicit user intent.

## Deterministic authority

This decision does **not** reduce Go authority.

Go remains responsible for:

- authorization;
- structural validity;
- canonical IDs;
- accounting/currency invariants;
- duplicate and reconciliation safety;
- domain compatibility;
- concurrency;
- canonical writes.

The distinction is:

```text
semantic meaning
!=
canonical safety
```

## Domain continuity

A review pauses a domain workflow; it does not strip domain identity.

A payslip requiring review remains a salary candidate and, after resolution,
must use the same salary finalizer as a clear payslip.

This principle applies to other domain events where review otherwise collapses a
specialized event into a generic transaction.

## Product trade-off

The accepted trade-off is:

```text
one trusted semantic owner
+ deterministic canonical constraints
+ exact residual review
```

instead of:

```text
multiple independent semantic opinions
+ generic "safe" review
```

Repeated consensus is not a substitute for evidence.

## Rejected alternatives

### Keep adding source-specific hotfixes

Rejected.

Examples include:

- patch one payslip period regex;
- copy receipt merchant-alias SQL into screenshot;
- add another generic review reason;
- special-case one batch confirmation.

These can fix a symptom while increasing cross-source drift.

### Let Go re-parse all model/user facts

Rejected.

This makes Go a second semantic interpreter and recreates the mismatch SAVR is
intended to remove.

### Trust model output without canonical guards

Rejected.

Semantic ownership is not canonical mutation authority.

### Add a universal fact database / ontology first

Rejected as YAGNI.

Existing typed structs, proposals, ReviewDecision, evidence, judgment rows, and
domain tables are sufficient until a concrete persistence gap proves otherwise.

### Always ask the user when validators disagree

Rejected.

A validator may be reporting representation or quality debt rather than missing
user knowledge.

## Business metrics

SAVR success is measured by:

- Known Fact Re-ask Rate;
- Validator-Induced Human Review Rate;
- Residual Fidelity Rate;
- Semantic Re-decision Rate;
- RHICE;
- post-auto-confirm material correction rate.

The initiative fails if it lowers model calls but increases human re-entry, or
lowers human interaction by weakening correctness.

## Product invariant

> **Understand once. Preserve accepted facts. Validate canonical safety. Ask only
> for the true residual.**
