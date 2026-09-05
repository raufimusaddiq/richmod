# ADR-035: One active household per user

## Status

Accepted — 5 September 2026.

## Decision

Richmod supports exactly zero or one active `household_member` row per user.
It does not support household switching, workspace selection, or concurrent
membership in multiple households. `household_id` remains the tenant key.

PostgreSQL enforces the invariant with a partial unique index on
`household_member(user_id) WHERE active = TRUE`. The migration first detects
duplicate active memberships and fails explicitly; it never selects or
deactivates a membership automatically. Inactive historical memberships remain.

Authentication exposes one nullable `household` object. Internally, a principal
has canonical household ID, role, and presence fields. Zero-household users may
authenticate and use platform-level super-admin routes, but household financial
routes reject them. More than one active membership is an internal invariant
violation: authentication fails closed and logs the user ID, request ID, and
error category without exposing database details.

Super Admin may add or reactivate a member only when the user has no active
household or is already active in the target household. An active membership in
another household returns `409 USER_ALREADY_HAS_HOUSEHOLD`; Richmod never moves
the user implicitly.

## Compatibility

`GET /api/v1/auth/me` changes from `memberships: []` to `household: object|null`.
The removed array was not a supported household selector. Frontend owner checks
use `household.role`. Internal legacy test construction remains temporarily
accepted through `ContextWithPrincipal`, but production tenant resolution never
uses membership ordering.

## Consequences

Existing valid production households need no user action. Bootstrap, Telegram,
email ingress, bank listeners, ledger, documents, reviews, analytics, budgets,
salary, and insights keep their existing household-scoped behavior.
