# ADR-025: Config-driven bank-email ingestion

## Status

Accepted — V4.

## Decision

Bank notification sources are configured as household-scoped listeners. Gmail
only establishes source trust and persists evidence; the worker uses one
required native `emit_bank_transaction` call, validates it in Go, and applies
the fixed `SPENDING_ONLY` policy. LLM output cannot mutate the ledger.

## Consequences

Adding a supported bank sender is a settings operation. Jago's deterministic
parser remains available during shadow rollout and as a temporary fallback,
while generic extraction evidence is retained for comparison and audit.

The rollout is controlled by `GMAIL_GENERIC_PRIMARY`: true is now the default
and lets the generic worker own every matched listener, including the legacy
Jago sender. False is an explicit compatibility rollback that queues generic
extraction as shadow evidence and leaves the deterministic Jago path canonical.
The legacy parser is never run alongside generic-primary for the same event.
