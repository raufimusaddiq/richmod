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

Logged-out visitors receive a public product explanation at `/` and a focused
email/password form at `/login`. Authenticated `/` remains the existing
Overview, while authenticated `/login` returns to `/`. The browser waits for
the existing session check before choosing public or authenticated content, so
neither surface flashes before authentication state is known.

The shared presentation layer provides loading skeletons, retryable error
notices, polite success toasts, visible keyboard focus, reduced-motion support,
and mobile transaction cards. Motion is CSS-only and never changes or hides a
financial value.

PostgreSQL and Go remain authoritative. Transaction filtering and analytics
ranges are query-backed and household-scoped. Recharts renders API values but
does not compute canonical totals. Transaction detail reads evidence and audit
history; document cards expose source, extraction summary, confidence, linked
transactions, and review state.

Budget handlers were later removed by ADR-051; the table remains. The active
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

## Addendum: explicit load and busy states

Pages do not use a bare `loading` boolean or a single `working` string. The
shared helpers in `apps/web/app/lib/useLoadState.js` define the pattern:

- `useGuardedLoad`: status `idle | loading | refreshing | error | ready`, an
  `AbortController` plus a sequence guard so a superseded response never sets
  state. Only a first load may show a skeleton; a reload is `refreshing` and keeps
  the rendered children mounted (a typed note or a focus hint survives it).
- `useDrawerRecord`: drawer state `closed | loading | error | ready` with an
  abortable request, so a slow response cannot reopen a closed drawer or mix two
  records.
- `useBusyMap`: one busy flag per key (card id, setting name); a second start for
  the same key is refused synchronously and the key is always released in `finally`.
- Every async handler clears the stale error first and uses try/catch/finally.
- Only a 401 from `/auth/me` signs the user out; a network error or 5xx keeps the
  session and retries. A failed list request is shown as an error with retry,
  never as an empty or zero result.

`apps/web/tests/product-alignment.test.mjs` guards these rules at source level.
