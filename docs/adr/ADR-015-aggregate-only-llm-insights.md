# ADR-015: Aggregate-only LLM insights

## Status

Accepted.

## Decision

Insight generation snapshots deterministic monthly aggregate facts in PostgreSQL
before enqueueing a job. Raw transactions, evidence, account identifiers, and user
messages are never sent to the model. The cloud gateway returns a strict Indonesian
narrative schema that Go validates and stores as non-authoritative text only.

Generation is limited to once per household period per hour. Completeness below
0.70 bypasses the gateway and returns a deterministic data-quality message. Insight
failures never alter financial state, and all requests/completions are audited.
