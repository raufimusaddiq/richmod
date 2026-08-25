# ADR-012: Reconciliation scoring policy

## Status

Accepted.

## Decision

Reconciliation uses deterministic candidate generation and scoring. Only one
unambiguous strong match may auto-link; all uncertainty goes to review.

For receipt and screenshot evidence, candidates must have the same household,
ledger type, IDR amount, confirmed status, and a timestamp within 72 hours. The
score starts at 0.45, adds 0.25/0.20/0.10 for a distance within 1/24/72 hours,
and adds 0.25 for an exact normalized merchant. Auto-link requires one score of
at least 0.90, no second candidate above 0.80, a known timestamp, and extraction
confidence of at least 0.90. Available receipt arithmetic must also balance.
