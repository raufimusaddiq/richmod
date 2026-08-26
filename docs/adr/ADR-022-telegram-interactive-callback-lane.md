# ADR-022: Dedicated Telegram interactive callback lane

## Status

Accepted — V3 P0 phase 1.

## Decision

Telegram inline callbacks are persisted as immutable `TELEGRAM_CALLBACK` source
events and queued as `PROCESS_TELEGRAM_CALLBACK` jobs on an explicit
`INTERACTIVE` lane. The webhook acknowledges Telegram only after durable capture,
then returns HTTP 204. Callback processing reuses deterministic review state
transitions and never converts button data into an LLM text request.

Jobs are classified into `INTERACTIVE`, `DEFAULT`, and `BACKGROUND` lanes. The
worker services the interactive lane before bounded batches of other work so
vision/document processing cannot indefinitely starve button actions.

## Consequences

- Callback acknowledgement is transport feedback, not proof of financial success.
- Replay remains idempotent through the existing source-event external ID.
- Telegram bot credentials are available to the API only for the narrow ACK call.
- Message editing/retry repair remains a follow-up V3 phase.
