# ADR-036: Wealth, Savings, and Cycle Reconciliation

- Status: Accepted
- Date: 2026-09-06

## Decision

Add wealth observations and savings intent as additive PostgreSQL schema extensions. `transaction` remains the cashflow ledger; `purpose` classifies confirmed transfers without turning them into expenses. Wealth snapshots remain point-in-time observations. Closed salary-cycle residuals remain review metadata, never synthetic transactions.

Universal `review_item` remains the canonical review subject. It gains `cycle_residual_case_id` and an exactly-one-subject constraint. `review_request` accepts residual/non-transaction projections while retaining legacy transaction-backed rows for rolling deployment. Active residual reviews are idempotent through a partial unique index.

Existing transaction purposes are conservatively backfilled: transfers become `INTERNAL_TRANSFER`; all other rows become `GENERAL`. No historical savings inference occurs. Existing salary, ledger, Telegram, and review flows remain owners of their current mutations.

Bank Email remains the frozen `SPENDING_ONLY` compatibility pipeline (`PROCESS_BANK_EMAIL`). Financial Provider Email is a separate generic native-LLM observation pipeline. Provider email is valid standalone evidence; bank notification is optional corroboration. One real event remains one canonical transaction with many evidence rows. Wealth value remains observation/snapshot state, never a transaction. Go performs household-scoped alias/entity resolution and reconciliation; provider names are fixture data, not production branches.

Each Financial Provider Email observation is independently staged and reviewed.
Financial-email review items and reconciliation cases bind to the exact staged
observation, not merely the parent source event. This permits one email to
contain several independently actionable cash movements or wealth values
without one review overwriting another.

Successful Financial Provider Email extraction is durable even when the native
tool returns zero observations. Reprocessing uses the staged result, skips
terminal observations, and never repeats semantic extraction. A staged Wealth
value owns at most one `wealth_observation`. Strong provider-reference reuse
also requires compatible Account, amount, time, transfer purpose, and Wealth
target; incompatible evidence goes to Review without creating a transaction.
Preview runs the same deterministic resolution and reconciliation planner as
production inside a zero-canonical-mutation path. Financial-email candidate
search retains its bounded overflow sentinel; more than ten candidates fails
closed and cannot be confirmed as a new transfer.

Cashflow uses one shared rule wherever Wealth and Analytics report it:
`income - (expense - refund)`. A refund reduces expense; it is not a negative
income nor a Wealth adjustment.

Wealth screenshots use the existing native document boundary with an additive
`extract_wealth_observation` tool. Go validates the observation and stores it
in the non-ledger `wealth_observation` queue plus a dedicated
`WEALTH_OBSERVATION_CONFIRMATION` review. Go may resolve an exact household
Wealth Account match; zero or multiple matches require the user to choose. The
LLM never supplies an ID. An observation only prefills one value in a complete
snapshot; it never creates a partial snapshot or transaction.

Snapshot correction preserves the original snapshot account membership. It
does not use the set of accounts active today, so later-created accounts never
appear in historical snapshots and later-inactive accounts remain correctable.
Correction updates snapshot items in place, preserving canonical item IDs, and
audits complete before/after item values. Transfer mutation boundaries share
one database-backed purpose/Wealth compatibility rule; internal transfers never
carry a canonical related Wealth Account.

Ambiguous cross-channel Telegram transfers persist a bounded
`transfer_reconciliation_case`: the interpreted source account, amount, time,
purpose, Wealth destination, and deterministic candidate transaction IDs.
Review exposes only those candidates. A user may merge one candidate or
confirm a distinct transfer; Go performs the mutation and attaches Telegram
evidence. Weak evidence never selects a transaction automatically.

Bank and Telegram transfer reviews may classify one outgoing event as
`ASSET_PURCHASE` by selecting one compatible active Wealth Account. This
preserves the original evidence on the same canonical transfer; it never
creates a second expense or synthetic Wealth value.

Analytics Overview and Wealth current savings share one current-period
resolver. It uses a confirmed primary salary cycle when available, otherwise
the Jakarta calendar month. Open periods end at the next Jakarta midnight;
database session timezone never defines the boundary.

## Compatibility

The migration is additive, preserves existing tables and rows, keeps legacy `review_request.transaction_id` rows valid, and permits non-transaction review projections. Application rollout must write `review_item` first for residual reviews and continue supporting legacy transaction-backed requests during the rolling window.

## Consequences

Cross-household ownership and link validation remain service-layer responsibilities; PostgreSQL foreign keys provide row identity only. Residual generation must run after salary commit through an idempotent job/catch-up path. Snapshot completeness, residual freshness, and allocation sums require transactional service validation.
