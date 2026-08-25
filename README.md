# Family Finance

Self-hosted household income and expense tracking. PostgreSQL is the canonical
financial state; Go performs all financial mutations.

## Current phase

Phase 1 is establishing the canonical ledger. The schema is IDR-only and uses
PostgreSQL `NUMERIC(20,0)` for all financial amounts. The next increment adds
the one-time owner bootstrap command and authenticated ledger mutation APIs.

All finance calendar logic and database sessions operate in `Asia/Jakarta`
(GMT+7), while persisted instants use `TIMESTAMPTZ`.

## First owner bootstrap

Once migrations have run, create the initial owner exactly once. Pass the
password through standard input so it is not recorded in shell history:

```text
printf '%s\n' 'use-a-unique-12-plus-character-password' | docker compose exec -T api /bootstrap --email owner@example.com --name 'Owner Name' --household 'My Household'
```

The command creates the owner and household atomically, seeds Indonesian
categories, and refuses to run again after an owner exists.

Web sessions expire after 24 hours of inactivity. Each authenticated request
renews the expiry by another 24 hours.

## Local startup

1. Copy `.env.example` to `.env` and replace every placeholder with secure values.
2. Run `docker compose up --build`.
3. Check `http://localhost:8080/healthz` and `http://localhost:8080/readyz`.

The compose ports bind to loopback only. Production uses the host-managed Caddy
instance for TLS at `finance.investdx.biz.id`; see the production deployment
runbook. Secrets must be supplied externally.
