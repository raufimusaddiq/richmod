# ADR-047 — Semantic Fact Ownership and Accepted-Fact Boundaries

## Status

Accepted — 2026-09-27.

## Context

ADR-045 defines when Richmod should use deterministic logic, Jev, generative
models, and humans.

It does not fully specify what happens when one layer has already established a
semantic fact and a downstream validator sees the same dimension again.

The audited code contains multiple patterns where a semantic dimension is
reinterpreted after it was already understood.

SAVR needs a precise boundary without introducing a generic semantic platform.

## Decision

Every semantic dimension has one current owner.

Owner classes:

```text
DETERMINISTIC_KNOWLEDGE
GENERATIVE_EXTRACTION
JEV
USER
```

A downstream layer may consume an accepted fact but cannot silently become a
second owner.

## Accepted fact

A fact is accepted when its owner has satisfied the source/domain acceptance
contract.

Examples:

- exact active merchant alias maps merchant → category;
- generative vision extraction produced a printed net-pay value that passed
  shape/source checks;
- Jev made a decisive bounded category choice among active categories;
- user explicitly supplied/corrected a transaction date.

Acceptance does not mean the canonical write is automatically allowed.

Go still performs canonical guards.

## Boundary contract

At a semantic-to-canonical boundary, the consumer receives:

- known semantic values;
- provenance/owner where needed;
- unresolved dimensions;
- conflict state where present.

The consumer may return:

- canonical-safe;
- exact validation consequence;
- residual dimensions.

It may not replace a known semantic value with an inferred alternative.

## No mandatory universal runtime type

This ADR intentionally does not require one global `AcceptedFact` interface or
database table.

Prefer:

1. existing source/domain typed structs;
2. small typed wrappers where a boundary currently loses ownership/provenance;
3. ReviewDecision for human residuals;
4. existing evidence/judgment/audit persistence.

Introduce a shared type only when at least two real boundaries benefit from the
same semantics and tests show duplication.

## Owner handoff

Owner handoff is valid only when:

- previous owner returned unresolved;
- user corrects a proposal;
- new independent evidence conflicts;
- canonical validation proves the value impossible and a repair obtains a new
  value.

A later model call that merely repeats the same question does not constitute a
handoff.

## Deterministic household knowledge

Exact household knowledge is `DETERMINISTIC_KNOWLEDGE`.

Resolvers for learned merchant/entity/account state should be shared across
compatible pipelines so one source does not "forget" what another source learned.

The resolver returns a fact only when the match is:

- household-scoped;
- active;
- unambiguous;
- allowed by the stored learning policy.

## User facts

An explicit user statement/confirmation/correction is `USER` authority.

After acceptance, downstream logic may validate the canonical representation but
must not run another semantic approval step without a distinct residual/conflict
reason.

## Intelligence routing interaction

ADR-045 remains authoritative.

Post-generative Jev is valid only for:

- a still-unresolved bounded dimension; or
- independent evidence verification.

SAVR does not ban multi-model workflows. It bans redundant ownership of the same
semantic claim.

## ReviewDecision interaction

ReviewDecision is the projection of the accepted/unresolved state to the human.

Therefore:

- accepted facts → known/proposed facts;
- unresolved facts → missing facts / bounded choices;
- conflicts → conflict-specific decision;
- human policy → explicit policy choice.

ReviewDecision MUST NOT invent a residual dimension that the event state does not
actually have.

## Consequences

Positive:

- fewer redundant model calls;
- fewer known-fact re-asks;
- source pipelines reuse learned household knowledge;
- provenance becomes explainable;
- Go remains strong canonical authority without becoming an NLP layer.

Cost:

- source/domain boundaries need explicit ownership audits;
- some old validators/regexes stop being semantic gates;
- tests must assert ownership and model-call order, not only final rows.

## Non-goals

This ADR does not introduce:

- semantic graph;
- event sourcing;
- new fact database;
- generic rule engine;
- model consensus;
- new AI authority.
