# ADR-025: Config-driven bank-email ingestion

## Status

Accepted — V4.

## Decision

Bank notification sources are configured as household-scoped listeners. Gmail
only establishes source trust and persists evidence; the worker uses one
required native `emit_bank_transaction` call, validates it in Go, and applies
the fixed `SPENDING_ONLY` policy. LLM output cannot mutate the ledger.

## Consequences

Adding any bank sender is a settings operation. There is no bank-specific
parser, sender environment variable, migration seed, model prompt, or
compatibility flag in the active ingestion path. Every matched listener uses
the same generic extraction and policy pipeline.
