# Family Finance

Self-hosted household income and expense tracking. PostgreSQL is the canonical
financial state; Go performs all financial mutations.

## Current phase

Phase 0 provides containerized PostgreSQL, API, worker, web scaffolding, and
forward-only Goose migrations. Phase 1 will add the canonical ledger and the
one-time owner bootstrap command.

## Local startup

1. Copy `.env.example` to `.env` and replace every placeholder with secure values.
2. Run `docker compose up --build`.
3. Check `http://localhost:8080/healthz` and `http://localhost:8080/readyz`.

The compose ports bind to loopback only. Production uses the host-managed Caddy
instance for TLS at `finance.investdx.biz.id`; see the production deployment
runbook. Secrets must be supplied externally.
