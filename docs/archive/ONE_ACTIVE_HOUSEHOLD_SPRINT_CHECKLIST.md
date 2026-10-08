# One active household sprint checklist

Source of truth: ADR-035, the explicit sprint request, and current repository behavior.

- [x] Audit membership reads and runtime writes across API, worker, and web.
- [x] Add a migration precheck that rejects duplicate active memberships.
- [x] Add a partial unique index for one active household per user.
- [x] Replace array-based principal tenant selection with canonical fields.
- [x] Represent zero-household users without selecting a tenant.
- [x] Fail authentication closed on multiple active memberships.
- [x] Add reusable `TenantFromPrincipal` resolution.
- [x] Replace production `Memberships[0]` role and household checks.
- [x] Update `/api/v1/auth/me` and frontend owner checks to one household.
- [x] Reject Super Admin cross-household member attachment with HTTP 409.
- [x] Preserve same-household idempotency and inactive target reactivation.
- [x] Preserve bootstrap and non-membership-writing Telegram paths.
- [x] Run migration against a disposable PostgreSQL database.
- [x] Run API tests and vet.
- [x] Run worker tests and vet.
- [x] Run frontend tests and production build.
- [x] Run final membership-ordering and diff checks.
- [x] Commit, push branch, merge, and push `main`.
