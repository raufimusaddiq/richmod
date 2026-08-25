# ADR-010: Transaction proposals before untrusted mutation

## Status

Accepted.

## Decision

Natural language, document extraction, and fallback parsing create transaction
proposals before any canonical ledger mutation.

One source event may contain multiple independently reviewable proposals, such as
the rows of a transaction-history screenshot. `proposal_key` identifies each row
without weakening source-event idempotency.
