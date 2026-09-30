# ADR-015: Aggregate-only LLM insights

## Status

Accepted.

## Decision

Insight generation snapshots deterministic monthly aggregate facts in PostgreSQL
before enqueueing a job. Raw transactions, evidence, account identifiers, and user
messages are never sent to the model. The cloud gateway returns a strict Indonesian
narrative schema with exactly one recommendation paragraph grounded in the supplied
aggregate snapshot. Go validates the schema and stores it as non-authoritative text only.

Generation is limited to once per household period per hour. Completeness below
0.70 bypasses the gateway and returns a deterministic data-quality message. Insight
failures never alter financial state, and all requests/completions are audited.

## Native-tool amendment — 2026-09-30

Generative Analytics output must use Richmod's native tool-call path with required,
strict, server-owned schemas. Free-form model prose is not an accepted Analytics
product contract. Any prose shown to users must arrive inside validated tool
arguments. Malformed/missing tool calls fail closed to deterministic Analytics;
there is no free-form fallback.

The Analytics Cycle Review initiative may evolve the output from one generic
narrative into structured evidence-bound findings, while preserving this ADR's
aggregate/structured-fact input boundary and non-authoritative model role.
