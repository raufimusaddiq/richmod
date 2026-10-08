# ADR-051 — Release retirement of unused capabilities

## Status

Accepted — 2026-10-03. Supersedes ADR-014. Supersedes the `shadow` and
`primary` interpretation stages of ADR-037; ADR-037's one-pass field repair
stays in force.

## Context

The release cleanup audit
([`audits/RELEASE-CLEANUP-AUDIT.md`](../audits/RELEASE-CLEANUP-AUDIT.md))
found three capabilities that ship code nobody can use:

- **Budgets (ADR-014).** ADR-019 removed every budget screen and request from
  the web app. The API handlers stayed, but no page, bot tool, insight or job
  read or wrote the `budget` table. The household does not use budgets.
- **HTTP routes with no caller.** `GET /api/v1/analytics/spending`,
  `GET/POST /api/v1/merchants`, `POST /api/v1/merchants/{id}/aliases` and
  `GET/POST /api/v1/admin/households/{householdId}/members` are not called by
  the web app, the bot or any script.
- **ADR-037 `shadow` and `primary` stages.** Production runs `legacy`.
  `primary` was compiled off. `shadow` made a second model call per document
  and stored only agreement counters that no feature or report read.

## Decision

Remove the budget handlers and routes, the uncalled routes above, and the
`shadow`/`primary` interpretation stages together with
`RICHMOD_DOCUMENT_INTERPRETATION`. Document intake is classify-then-extract
with ADR-037's validator feedback and at most one field-restricted repair call.

Kept on purpose: `POST /api/v1/transactions/{id}/void`,
`POST /api/v1/transactions/{id}/confirm` and
`POST /api/v1/reconciliation-merges/{id}/reverse`. They are correction tools
the owner may need by hand, even though no screen calls them yet.

No migration. The `budget` table and historical `INTERPRETATION_SHADOW*`
extraction rows stay, because canonical and audit history is never
hard-deleted; nothing reads or writes them.

## Consequences

- Fewer routes to authorize and test; one fewer model call per document when
  shadow was enabled.
- A deployment that still sets `RICHMOD_DOCUMENT_INTERPRETATION` is unaffected:
  the variable is no longer read.
- Bringing budgets or typed interpretation back needs a new ADR, a UI or
  consumer, and new code; the removed code is in git history.
