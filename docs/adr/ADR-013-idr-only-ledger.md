# ADR-013: IDR-only ledger

## Status

Accepted.

## Decision

The MVP canonical ledger accepts Indonesian rupiah (`IDR`) only. Amounts use
PostgreSQL `NUMERIC(20,0)` and every transaction row has a database constraint
requiring `currency = 'IDR'`. Multi-currency conversion and reporting are out of
scope for MVP and require a future forward-only migration and ADR.
