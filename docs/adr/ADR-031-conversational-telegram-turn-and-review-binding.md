# ADR-031: Conversational Telegram turns and contextual review binding

## Status

Accepted — 2026-09-01. **Amended by ADR-033 on 2026-09-12.**

## Decision

Telegram free-text turns receive server-owned bounded context: recent user,
assistant, and tool turns; allowed categories; pending states; and active review
context. Exact callback and reply bindings remain strongest. One uniquely active
review may bind normal free text; multiple candidates require clarification.

Canonical IDs remain server-private. `review_item` remains canonical; Telegram
turns and review delivery are projections only.

ADR-033 changes the conversational execution model: one native tool decision no
longer terminates the entire free-text turn. A turn may contain multiple bounded
model phases and validated READ batches. At most one Go-owned side effect may
execute in the turn.

## Consequences

- Assistant/tool context persists in `telegram_conversation_turn`.
- A model phase may return ordinary text, a validated READ batch, or exactly one
  side-effect tool as defined by ADR-033.
- Review resolution remains household-scoped, audited, and deterministic.
- Required review facts are collected without guessing. Later actions stay
  unavailable until their server-owned prerequisites exist.
- Exact callback/reply bindings remain authoritative even though the surrounding
  conversation is now model-driven.
- Telegram Wealth observations reply to the uploaded image; the user's follow-up
  is resolved through the existing review domain boundary.
- A mistaken balance extraction may become `ASSET_PURCHASE` only after Go
  resolves user-supplied source-account and transaction-time facts. The original
  Telegram image becomes transaction evidence; its Wealth observation is dismissed.
- Backfilled transactions preserve an explicitly stated Jakarta date. Named
  time periods are normalized by Go, not guessed by the model: `PAGI` 09:00,
  `SIANG` 13:00, `SORE` 17:00, and `MALAM` 20:00. Proposal metadata records
  `APPROXIMATE` precision; exact user-supplied `HH:MM` remains `EXACT`.
