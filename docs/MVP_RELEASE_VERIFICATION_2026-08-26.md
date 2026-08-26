# MVP release verification — 2026-08-26

## Release

- Public frontend: `https://finance.investdx.biz.id`
- Main release at verification: `f5fd3d8`
- Ledger currency: IDR
- Household/database timezone: `Asia/Jakarta` (GMT+7)
- Database schema: Goose version 14

## Automated verification

- Full Go API tests passed and `go vet ./...` passed.
- Full Go worker tests passed and `go vet ./...` passed.
- Frontend Node test suite passed: 7/7.
- Next.js production build passed and prerendered all seven product routes.
- Production Compose configuration validation passed.
- A disposable PostgreSQL 17 database migrated successfully from version 1
  through version 14; the disposable database and network were removed.
- Repository diff checks passed and the `main` worktree was clean.

## Production verification

- `/healthz` returned healthy and `/readyz` returned ready.
- `/`, `/transactions`, `/analytics`, `/reviews`, `/documents`, `/household`,
  and `/settings` each returned HTTP 200 through Caddy.
- HSTS, `nosniff`, frame denial, and same-origin referrer policy headers were
  present on the public response.
- PostgreSQL reported schema 14, 12 succeeded jobs, zero pending/failed jobs,
  zero open reviews, and no implicitly learned active merchant rules.
- The current worker heartbeat was fresh in GMT+7.
- Telegram `getWebhookInfo` reported no pending updates or delivery error and
  explicitly allowed `message` and `callback_query`.
- Gmail Pub/Sub authenticated retries changed from HTTP 400 to HTTP 204 after
  accepting the live numeric `historyId` representation. Seven queued history
  jobs succeeded on their first attempt, and the connected mailbox advanced to
  history ID `98027` with an active watch through 2026-09-01.

## Product review

The public product retains a calm green/neutral financial visual system,
consistent page hierarchy, IDR labels, and visible review state. Desktop has a
persistent routed sidebar; mobile has bottom navigation and card-form ledger
rows. Loading skeletons, retryable errors, polite success toasts, visible focus,
empty states, and reduced-motion behavior are present without obscuring or
animating financial values.

## Workflow evidence

Each cohesive implementation used a feature/fix branch and linked worktree,
passed relevant tests, was committed and pushed, merged into `main` with
`--no-ff`, pushed to origin, deployed from the updated main worktree, and then
verified against production.
