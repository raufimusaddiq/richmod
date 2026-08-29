# Chart Elapsed-Days Fix — Completion Checklist

Fix dense salary-cycle chart data showing future zero-value days as blank gaps.
Do not modify source chart-refinement plan or its implementation checklist.

## Scope

- [x] Confirm `/api/v1/analytics/cycle/daily` returns all dates in active cycle plus `daysElapsed`.
- [x] Keep API and database contracts unchanged.
- [x] Trim only displayed daily cycle data to elapsed dates.
- [x] Apply same rule to Ringkasan and Analisis cycle mode.
- [x] Preserve full data if older response lacks `daysElapsed`.
- [x] Add pure-helper regression coverage for elapsed, zero, and missing elapsed-day values.

## Verification

- [x] Run web tests: `npm test` — 19 passed in Node 22.23.2 container.
- [x] Run production web build: `npm run build` — passed.
- [x] Inspect diff for unrelated changes.
- [ ] Commit, push, merge to `main`, and deploy web.

## Completion record

```text
Baseline main SHA: aa2ec0c01583e5ea3f10579401066eb29684bfb4
Branch: fix/chart-elapsed-days
Worktree: /home/ubuntu/richmod-worktrees/chart-elapsed-days
API changes: None.
Tests: `npm test` — 19 passed; `npm run build` — passed.
```
