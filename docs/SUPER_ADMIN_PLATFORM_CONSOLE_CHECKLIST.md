# Super Admin Platform Console — Completion Checklist

Source of truth: `docs/RICHMOD_SUPER_ADMIN_PLATFORM_CONSOLE_CODEX.md`.
This checklist tracks implementation only. Source PRD is unchanged.

## Backend

- [x] Add additive `platform_audit_log` migration and indexes.
- [x] Keep all admin routes behind session and Super Admin authorization.
- [x] Add overview endpoint with worker, lane, failure, review, integration, and LLM metadata.
- [x] Add bounded jobs list/detail endpoints with retry history and allowlisted references.
- [x] Add LLM summary/calls endpoints with metadata-only responses.
- [x] Add structured operational logs endpoint from persisted safe sources.
- [x] Add household list/detail overview endpoints without financial content.
- [x] Add platform and household audit endpoints.
- [x] Make user activation/Super Admin changes transactional and auditable.
- [x] Block self-lockout and removal/deactivation of final active Super Admin.
- [x] Make household member creation transactional and auditable.
- [x] Add focused Go tests for cursor and payload-reference redaction.
- [ ] Add DB-backed integration tests for every endpoint and final-admin invariant.
- [x] Run migration Up block against disposable PostgreSQL and inspect platform-audit query plan.

## Frontend

- [x] Add URL-query tabs: Overview, Jobs, LLM, Logs, Households, Users, Audit.
- [x] Add responsive admin metrics, lane status, tables, and detail drawers.
- [x] Keep job payload and LLM/source content out of rendered UI.
- [x] Add explicit confirmation for user privilege/status changes.
- [x] Add local tab loading/error/empty states and refresh actions.
- [x] Add mobile horizontal scrolling for dense tables and stacked metrics.
- [x] Add focused frontend tests for tab routing, redaction, and mutation confirmation.
- [ ] Run desktop, tablet, and mobile browser review with keyboard navigation.

## Verification

- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `cd apps/web && npm test`
- [x] `cd apps/web && npm run build`
- [x] Inspect diff and verify source PRD has no changes.

## Release state

- [ ] Commit feature branch.
- [ ] Push feature branch.
- [ ] Merge with `--no-ff` into `main`.
- [ ] Push updated `main`.
- [ ] Deploy only after explicit approval.

## Known caveats

- Cursor pagination is bounded and API-compatible; follow-up can add shared cursor envelopes to all list endpoints.
- Overview and list query plans need disposable-DB verification before production rollout.
- No deployment performed in this iteration.
