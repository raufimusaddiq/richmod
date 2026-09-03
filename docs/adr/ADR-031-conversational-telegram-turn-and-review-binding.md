# ADR-031: Conversational Telegram turns and contextual review binding

## Status

Accepted — 2026-09-01.

## Decision

Telegram free-text turns receive server-owned bounded context: recent user,
assistant, and tool turns; allowed categories; pending states; and active review
context. Exact callback and reply bindings remain strongest. One uniquely active
review may bind normal free text; multiple candidates require clarification.

Canonical IDs remain server-private. `review_item` remains canonical; Telegram
turns and review delivery are projections only.

## Consequences

- Assistant/tool context persists in `telegram_conversation_turn`.
- One native tool decision terminates each free-text model phase.
- Review resolution remains household-scoped, audited, and deterministic.
- Required review facts are collected in order. Later actions such as category
  selection stay unavailable until an unknown merchant has been supplied.
