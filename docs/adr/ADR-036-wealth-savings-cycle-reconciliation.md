# ADR-036: Wealth, Savings, and Cycle Reconciliation

- Status: Accepted
- Date: 2026-09-06

## Decision

Add wealth observations and savings intent as additive PostgreSQL schema extensions. `transaction` remains the cashflow ledger; `purpose` classifies confirmed transfers without turning them into expenses. Wealth snapshots remain point-in-time observations. Closed salary-cycle residuals remain review metadata, never synthetic transactions.

Universal `review_item` remains the canonical review subject. It gains `cycle_residual_case_id` and an exactly-one-subject constraint. `review_request` accepts residual/non-transaction projections while retaining legacy transaction-backed rows for rolling deployment. Active residual reviews are idempotent through a partial unique index.

Existing transaction purposes are conservatively backfilled: transfers become `INTERNAL_TRANSFER`; all other rows become `GENERAL`. No historical savings inference occurs. Existing salary, ledger, Telegram, and review flows remain owners of their current mutations.

Cashflow uses one shared rule wherever Wealth and Analytics report it:
`income - (expense - refund)`. A refund reduces expense; it is not a negative
income nor a Wealth adjustment.

Wealth screenshots use the existing native document boundary with an additive
`extract_wealth_observation` tool. Go validates the observation and stores it as
document evidence plus a pending review; it never resolves a Wealth Account by
LLM output or creates a partial snapshot. A user must confirm a complete
full-account snapshot before canonical Wealth state changes.

## Compatibility

The migration is additive, preserves existing tables and rows, keeps legacy `review_request.transaction_id` rows valid, and permits non-transaction review projections. Application rollout must write `review_item` first for residual reviews and continue supporting legacy transaction-backed requests during the rolling window.

## Consequences

Cross-household ownership and link validation remain service-layer responsibilities; PostgreSQL foreign keys provide row identity only. Residual generation must run after salary commit through an idempotent job/catch-up path. Snapshot completeness, residual freshness, and allocation sums require transactional service validation.
