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

## Bank Jago

Jago uses `SPENDING_ONLY` policy.

- Merchant/QR/debit-card outgoing -> expense candidate.
- Incoming Jago money -> not income.
- Transfers to known own/household accounts -> transfer, not expense.
- Jago pocket movement -> internal transfer, not expense/income.
- RDN/investment movement -> ignore from MVP spending.
- Parse known Jago email templates deterministically first; use LLM only as fallback.

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

## MVP scope

Implement income and expense tracking only.

Do not implement assets, stocks, mutual funds, gold, crypto, broker integrations,
portfolio valuation, investment P&L, or market-price feeds.

## Development rules

- Keep HTTP handlers thin; business rules belong in domain/service code.
- Money uses PostgreSQL `NUMERIC`, never float.
- Time uses `TIMESTAMPTZ`; household timezone is `Asia/Jakarta`.
- Schema changes require migrations and relevant tests.
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
