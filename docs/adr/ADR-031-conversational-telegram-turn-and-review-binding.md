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
- Missing merchant or purpose reviews ask for a direct reply and expose only an
  ignore action; Go advances their state after validating the bound reply.
- Telegram Wealth observations reply to the uploaded image; the user's follow-up
  is resolved through the existing native review tool.
- A mistaken balance extraction may become `ASSET_PURCHASE` only after Go
  resolves user-supplied source-account and transaction-time facts. The original
  Telegram image becomes transaction evidence; its Wealth observation is dismissed.
- Backfilled transactions preserve an explicitly stated Jakarta date. Named
  time periods are normalized by Go, not guessed by the model: `PAGI` 09:00,
  `SIANG` 13:00, `SORE` 17:00, and `MALAM` 20:00. Proposal metadata records
  `APPROXIMATE` precision and the confirmation tells the user the time is
  approximate; exact user-supplied `HH:MM` remains `EXACT`.
