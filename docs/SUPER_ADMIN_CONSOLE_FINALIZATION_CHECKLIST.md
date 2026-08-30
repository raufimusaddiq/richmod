# Super Admin Console Finalization Checklist

Source of truth: `docs/RICHMOD_SUPER_ADMIN_CONSOLE_FINALIZATION_CODEX.md` (unchanged).

## Release status

- [x] Platform audit stores normal middleware request IDs as text (migration 35).
- [x] Bounded keyset pagination added to LLM calls, logs, platform audit, household audit.
- [x] Server-side range/filter parameters retained for operational lists.
- [x] Overview exposes platform health, lane/LLM metrics, and recent structured events.
- [x] Household detail exposes members, integrations, recent jobs/LLM, failed sources, and audit.
- [x] Jobs, logs, and LLM expose server-backed filters, refresh, and cursor loading.
- [x] Platform/household audit views expose filters, cursor loading, request ID, and human-readable summaries.
- [ ] LLM calls-by-hour chart.
- [ ] Filter values mirrored into browser URL.
- [ ] DB-backed admin integration coverage and query-plan review.
- [ ] Disposable migration and production smoke verification.

## Verification log

| Check | Result | Date |
|---|---|---|
| API tests/vet | passed | 2026-08-30 |
| Web tests/build | passed (29 tests) | 2026-08-30 |
| Disposable Goose migration | passed through version 35; request_id is text | 2026-08-30 |
| Production deployment | pending | 2026-08-30 |
| Safe cache reclaim | pending | 2026-08-30 |

Remaining unchecked items are explicit follow-up, not claimed shipped.
