# Wealth, savings, and cycle reconciliation release checklist

Source of truth: ADR-036, the immutable PRD sections 76–77, and current repository behavior.

- [ ] Confirm migration `00046` applies on a disposable PostgreSQL database.
- [ ] Confirm existing transactions and legacy transaction-backed review requests remain valid.
- [ ] Confirm transfer backfill uses `INTERNAL_TRANSFER`; all other historical purposes use `GENERAL`.
- [ ] Confirm wealth snapshots remain observations and create no cashflow transactions.
- [ ] Confirm savings intent is explicit; no historical savings inference runs.
- [ ] Confirm a closed salary-cycle residual creates or reuses one active review item/request.
- [ ] Confirm residual retries/catch-up remain idempotent and create no synthetic transactions.
- [ ] Confirm cross-household ownership and allocation validation fail closed.
- [ ] Run API tests and vet.
- [ ] Run worker tests and vet.
- [ ] Run frontend tests and production build.
- [ ] Run `scripts/check_native_only_llm.sh`.
- [ ] Validate Compose and CI workflow syntax.
- [ ] Deploy immutable images; complete production wealth/residual smoke checks and worker monitoring.
- [ ] Retain forward schema on any application rollback; repair only through a new migration.
