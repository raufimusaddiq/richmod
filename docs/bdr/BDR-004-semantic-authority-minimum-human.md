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

Richmod will use **single semantic ownership per fact/dimension** and **minimum
sufficient intelligence**.

A source/domain may reach semantic sufficiency through Go-only deterministic
knowledge, Jev-only bounded judgment, LLM-only arbitrary understanding, an
LLM→Jev residual handoff, or explicit user authority. Repeated model consensus is
not the default.

Once a source-specific acceptance contract accepts a semantic fact, downstream
code may:

- validate canonical safety;
- map it to canonical IDs;
- detect independent conflict;
- preserve provenance;
- or reject an impossible representation.

Downstream code may not silently re-understand the same semantic dimension.

## Human-effort materiality

Only uncertainty that can change the attempted canonical financial outcome,
satisfy a hard canonical invariant, resolve a material evidence conflict, or
obtain irreducible household policy may increase RHICE.

A precise residual that cannot change the outcome is still unnecessary human
work.

Generic confidence, payment-mechanism metadata, redundant source hints, and
other quality/provenance dimensions stay non-blocking after material semantic
sufficiency unless a source contract proves they affect the canonical result.

## User authority

An explicit user confirmation or correction is semantic authority unless:

- the value violates a hard canonical invariant;
- the referenced canonical entity is invalid/foreign/inactive;
- a concurrent canonical decision already won;
- new independent evidence creates a material conflict.

Jev is not an approval committee over explicit user intent.

## System-derived policy authority

A deterministic source policy may establish a canonical fact when no better
source-observed fact exists.

Example: a bank email with no printed transaction timestamp uses the email
`received_at` as canonical `transaction_at` with
`EMAIL_RECEIVED_AT` provenance. This is not missing data and must not create a
human time question.

Policy-derived facts remain auditable and must never be presented as if the
source printed them.

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

### Hard-gate every uncertain signal

Rejected.

Correctness means blocking material unresolved risk, not blocking every
non-material metadata or confidence uncertainty. Turning QR-vs-card mechanism,
generic confidence, or redundant source hints into required human work violates
the minimum-interaction north star.

### Treat machine inability as a human task

Rejected.

Malformed model/schema output, provider outage, or parser/representation failure
stays retry/repair/infrastructure state unless a material fact or policy genuinely
requires the household.

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

> **Understand once. Use the minimum sufficient intelligence. Preserve accepted
> facts. Validate canonical safety. Ask only for a material residual.**


---

## 2026-09-28 closure amendment — measurement on the real owner household

This amendment is authoritative for SAVR closure and is defined in
`docs/RICHMOD_UIR_SAVR_CLOSURE_PRD.md` / BDR-005.

Richmod currently has one real production household and its product owner is the
primary user. SAVR production validation therefore uses ordinary real
owner-household usage rather than requiring a disposable production household or
synthetic financial traffic.

The business metric previously named **Residual Fidelity Rate** is narrowed for
the current product stage to **Residual Contract Fidelity**:

> the stored ReviewDecision must ask only for dimensions that are unresolved by
> its own accepted-fact, consequence, conflict/ambiguity, or human-policy
> contract, and completion must not require undeclared semantic input.

This supersedes the assumption that SAVR needs a manually labelled semantic
ground-truth dataset before product closure.

Validator-Induced Human Review Rate and Semantic Re-decision Rate remain required
business signals. Their missing provenance is an implementation gap to close,
not a reason to create a new semantic platform.

Historical rows that predate the required provenance are unknown/incomplete, not
zero.
