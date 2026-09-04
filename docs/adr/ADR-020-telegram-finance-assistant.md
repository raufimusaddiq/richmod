# ADR-020: Telegram finance assistant queries and inline review actions

## Status

Accepted — 2026-08-26.

## Context

Telegram already accepted income/expense text, images, and replies bound to a
review message. The aligned MVP also requires finance-scoped queries,
correction, review listing, and faster inline review controls without allowing
the LLM to select arbitrary records or invent financial totals.

## Decision

- One strict gateway extraction classifies supported intents and emits only
  bounded parameters: Jakarta date period, search text, and explicit correction
  fields.
- Go validates those parameters. Household-scoped PostgreSQL queries select
  records and calculate authoritative IDR totals. Query result rows and totals
  are not sent back to the LLM for wording.
- Searches return at most six recent matching records. Custom disclosure ranges
  are capped at one year. A correction mutates only when its bounded search has
  exactly one result and records a Telegram audit event.
- Telegram callback data contains only an allowlisted action or category slug.
  It never contains a transaction ID. The worker resolves the transaction from
  the unique household/chat/message binding already stored on `review_request`
  and re-authorizes the numeric Telegram identity.
- Category, transfer, income confirmation, and merchant-rule prompts use inline
  keyboards. Existing natural-language, reply-to-message review handling stays
  supported.
- `UPLOAD_FINANCIAL_DOCUMENT` directs the member to the existing generic image
  and document pipeline; it does not create a separate receipt path.

## Consequences

The assistant remains useful when response wording is deterministic and cannot
disclose another household's ledger. Ambiguous searches or corrections require
a follow-up. Callback security reuses the existing exact review binding, so no
schema or new infrastructure is required.
