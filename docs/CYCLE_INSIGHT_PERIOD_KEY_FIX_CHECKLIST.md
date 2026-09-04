# Cycle Insight Period Key — Completion Checklist

Fix for salary-cycle insight generation failing when cycle start is not first day
of calendar month.

## Cause

- [x] Confirm production constraint `insight_period_check` required day one.
- [x] Confirm cycle anchor was passed as `insight.period` and failed insert.
- [x] Confirm insert error was incorrectly returned as duplicate generation.

## Fix

- [x] Store real cycle anchor in `insight.period`.
- [x] Remove month-only period constraint with additive migration `00038`.
- [x] Scope pending uniqueness and existing lookup by household, stored period,
  period kind, and period start.
- [x] Keep calendar and cycle insight keys distinct.
- [x] Preserve exact requested cycle kind even when salary anchor falls on first
  day of month.
- [x] Return `409` only for PostgreSQL unique violations; surface other insert
  failures as `500`.

## Verification

- [x] Migrate clean disposable PostgreSQL through `00038`.
- [x] Integration test: August 24 salary anchor generates one pending insight and
  one `GENERATE_INSIGHT` job; repeat request returns `200 EXISTING`.
- [x] API insight tests and vet pass in pinned Go container.
- [ ] Commit, push, merge, and deploy after explicit approval.
