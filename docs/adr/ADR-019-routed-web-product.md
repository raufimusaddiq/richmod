# ADR-019: Routed web product and dormant budgeting

## Status

Accepted.

## Context

The original single dashboard mixed document intake, review, analytics,
transactions, insights, and budgeting. It did not provide enough room for
ledger provenance, filters, household management, or transparent system state.

## Decision

The active web product uses a shared responsive shell with routes for Overview,
Transactions, Analytics, Review Inbox, Documents, Household, and Settings.
Desktop uses persistent side navigation and mobile uses bottom navigation.
Authentication continues to use the existing sliding server session on every
route.

PostgreSQL and Go remain authoritative. Transaction filtering and analytics
ranges are query-backed and household-scoped. Recharts renders API values but
does not compute canonical totals. Transaction detail reads evidence and audit
history; document cards expose source, extraction summary, confidence, linked
transactions, and review state.

Budget handlers and tables remain intact for possible future use, but the active
frontend contains no budget navigation, requests, forms, or utilization display.
No destructive migration is made solely to remove budgeting from this product
iteration.

Settings exposes existing account, category, known-account, merchant,
integration, and operational capabilities. Lifecycle changes disable records
instead of deleting history. Bank Jago accounts are constrained to
`SPENDING_ONLY`.

## Consequences

- Product areas can evolve without returning to a single oversized page.
- Financial calculations remain deterministic and independently testable.
- Provenance and ambiguous state are visible to household users.
- The charting dependency is a presentation dependency only.
