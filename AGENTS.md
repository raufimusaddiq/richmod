# AGENTS.md

## Project docs

Before material changes, read the relevant current documentation under `docs/`.

Priority when requirements conflict:

1. latest explicit user instruction;
2. current repository behavior/state;
3. current project documentation and ADRs;
4. older/historical specs.

When behavior or architecture changes, update the relevant docs in the same branch.
Do not treat an initial design document as permanently authoritative.

## Stack

- Backend: Go
- Frontend: Next.js + React + JavaScript
- Database: PostgreSQL
- Jobs: PostgreSQL-backed queue
- LLM: Cloud LLM Gateway only
- No Python, Java, Ollama, Redis, Kafka, RabbitMQ, or vector DB unless explicitly approved

## Core rules

- PostgreSQL is the canonical financial state.
- Go owns all financial state transitions.
- LLM output is untrusted and must pass deterministic validation before DB mutation.
- LLM must never directly access or mutate the database.
- Preserve source events/evidence; dedup links evidence instead of deleting it.
- Never hard-delete canonical financial records.
- Ambiguity goes to Review Inbox instead of being guessed.
- Financial mutations must be auditable and household-scoped.
- Webhooks and jobs must be idempotent.
- Deterministic flows must keep working when the LLM gateway is unavailable.

## Bank email ingestion

Bank email sources are configured as household-scoped listeners and all use
the generic native-tool extraction pipeline with fixed `SPENDING_ONLY` policy.
Do not add bank-specific parsers, sender environment variables, or provider
branches to the active ingestion path. Ambiguous or incomplete facts go to
review and are never guessed.

## Telegram

- Authorize by numeric Telegram user ID.
- Telegram is finance-only, not a general-purpose agent.
- Ambiguous transactions may trigger interactive review.
- Bind review replies using `reply_to_message_id` / stored Telegram message ID.
- Never let the LLM guess transaction identity when deterministic binding exists.

## Images and documents

All finance images use one generic document pipeline: payslip, receipt, bank/e-wallet screenshot, transfer proof, invoice, transaction history, etc.

```text
source_event -> LLM structured extraction -> Go validation -> proposal -> reconciliation -> transaction or review
```

- Payslip net pay may become income after validation.
- Payroll deductions are not automatically household expenses.
- Receipt evidence should enrich an existing matching transaction instead of creating duplicates.
- Invoice/bill alone is not proof of payment.

## V1 scope

V1 covers household income and expense tracking plus additive wealth observations,
savings intent classification, and salary-cycle residual reconciliation. The
cashflow ledger remains transaction-based; wealth snapshots are point-in-time
observations; residuals remain review metadata rather than synthetic transactions.

Wealth V1 supports manual, snapshot-level balances for bank, cash, e-wallet,
mutual fund, gold, brokerage, deposit, crypto, and loan/liability accounts.
Explicit non-goals: live NAV or market-price feeds, broker or wallet sync,
per-security positions, cost basis, realized/unrealized P&L, TWR, XIRR,
investment advice, historical savings inference, and automatic conversion of
residuals into transactions.

Bank Email is the frozen `SPENDING_ONLY` compatibility pipeline. Financial
Provider Email is a separate generic native-LLM observation pipeline. Provider
email is valid standalone evidence; bank email is optional corroboration. Go
owns household-scoped entity aliases, reconciliation, and canonical mutations.
One real event has one canonical transaction with many evidence rows. Wealth
values remain observations/snapshots, never transactions. Do not add
provider-specific production branches or expose canonical IDs to the LLM.

## Development rules

- Keep HTTP handlers thin; business rules belong in domain/service code.
- Money uses PostgreSQL `NUMERIC`, never float.
- Time uses `TIMESTAMPTZ`; household timezone is `Asia/Jakarta`.
- Schema changes require migrations, relevant tests, and a same-branch update to
  `docs/DATABASE_SCHEMA.md` (schema reference and ERD).
- Do not materially change architecture or add infrastructure without an ADR.
- Never commit or log secrets.
- Run relevant tests before marking work complete.

## Required worktree workflow

Every request that changes repository files must use its own branch and linked worktree unless the user explicitly overrides this rule.

1. Start from current `main` and create a descriptive branch/worktree, e.g.:

   ```bash
   git worktree add -b feat/short-description ../family-finance-worktrees/short-description main
   ```

2. Make all edits and verification inside that worktree.
3. Keep unrelated user changes out of the branch; never reset/clean/stash them without permission.
4. Commit with a descriptive message.
5. Push the feature branch.
6. Merge into `main` with `git merge --no-ff <branch>` and push `main`.
7. Deploy only from the updated `main` worktree.

Before finishing, inspect the diff, run relevant tests, and report the branch, commit,
merge result, and deployment result if applicable.

For the full sprint sequence—CI/release-image gate, safe reclaim, human deployment
approval, post-deploy verification, and handoff—follow
[`docs/runbooks/sprint-delivery.md`](docs/runbooks/sprint-delivery.md).
