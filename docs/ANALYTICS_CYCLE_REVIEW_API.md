# Cycle-review facts API

Sprint 1 of [the cycle-review plan](plans/analytics-cycle-review-sprint.md).

`GET /api/v1/analytics/cycle-review?cycle_start=YYYY-MM-DD` requires a session
with a selected household. Omit `cycle_start` for the active cycle. Invalid dates
return 400; dates without a confirmed primary salary anchor return 404. Dates
from another household never bind this endpoint. There is no household-ID input.

## Financial policy

- Salary anchors use distinct confirmed pay dates of the active primary salary
  source, matching `salary_cycle_bounds`. Future anchors are excluded.
- Cycle boundaries are Jakarta midnight; end is exclusive. Active salary cycles
  have no guessed end; `measuredUntil` is tomorrow's Jakarta date. Closed cycles
  end at the next observed confirmed salary anchor.
- Without an anchor, the default response is the elapsed calendar month with a
  `MISSING_SALARY_ANCHOR` blocker. No historical baseline is invented.
- Only confirmed transactions contribute. Expense is gross expense minus
  refunds; net cashflow is income minus this net expense. Transfers never enter
  expense or income. Savings allocation includes only confirmed transfers with
  `SAVINGS_TRANSFER`, `INVESTMENT_CONTRIBUTION`, or `ASSET_PURCHASE` purpose.
- Unallocated surplus is net cashflow minus confirmed savings transfers. It is
  not a synthetic transaction. Residual-review attributions are not additional
  transfers and do not change that arithmetic.
- All amounts remain whole-IDR decimal strings. Daily averages use two decimal
  places; ratios use four. No financial arithmetic uses binary floats.

## Comparison and drivers

Closed cycles compare with up to three preceding completed salary cycles.
Active cycles use the same elapsed-day prefix of completed cycles; a historical
cycle shorter than that prefix is ineligible. `comparison.mode` distinguishes
`FULL_CYCLE` from `ELAPSED_DAYS`. The comparison periods expose their measured
cutoff, including when it is before their full cycle end.

`median3Available` is true only with three eligible completed cycles. Missing
categories/merchants within an available comparison cycle contribute zero, not
missing history. No history means null deltas and ratios, not an invented zero
baseline. Refunds reduce category and merchant expense in every comparison.

Category changes expose current, previous, median, absolute/relative delta,
share of net expense, and signed contribution to total expense change.
Contribution is null when total change is zero. Relative delta is null for a
nonpositive denominator. A tiny positive denominator retains its exact baseline
and absolute delta alongside the ratio; the API does not assign significance.

Changes sort by absolute previous-cycle delta, or current amount without
history, then name/ID for deterministic ties. Merchant drivers are bounded to
10 overall and 10 per category; supporting confirmed expense/refund transactions
are bounded to 10 per category, ordered by amount then date/ID. Transaction type
stays explicit so a refund is not presented as an expense. Member attribution
sorts by name, not by spending, and includes shared/automatic/unattributed rows.

## Wealth and quality

The current snapshot is the latest observation before the measured cutoff
(capped at request time for an active cycle). The previous snapshot is the latest
observation before cycle start. No snapshot from after the selected cycle enters
its review. Each snapshot exposes observation time and age in Jakarta days.

Wealth movement reconciles the **observation interval**, not an invented cycle
end balance: net-worth delta minus confirmed cashflow in `(previous, current]`
is valuation/other change. Transfers are excluded from that cashflow, matching
the existing Wealth summary. A missing snapshot, unchanged snapshot, or changed
account set leaves reconciliation unavailable. This is not investment P/L or
an inferred savings transfer.

Concrete blockers include open/pending reviews (deduplicated against unresolved
transactions), uncategorized confirmed expense with amount, incomplete source
processing, missing salary anchor, missing snapshots, observations preceding the
cycle, and changed Wealth account sets. Source-only loose ends are scoped by
receipt time when no transaction/proposal date is available. No arbitrary
"stale after N days" threshold, AI confidence score, or semantic label is added.

## Consistency and consumers

All queries run in one read-only `REPEATABLE READ` transaction. The response
includes `cycle-review-v1` and generation time. It is recomputed, not cached:
historical canonical corrections are visible on the next read. No new schema,
financial mutation, provider call, or production-data repair is involved.

The existing Analytics page is unchanged in Sprint 1. Shared model-facing READ
tools, Web UI wiring, meeting mode, and decision persistence belong to subsequent
scoped sprints; the API's canonical IDs must not be forwarded to models.

## Verification

`apps/api/internal/analytics/review_test.go` covers median/outlier/tiny-baseline
arithmetic and absence of semantic output fields. `review_integration_test.go`
covers no/one/three-cycle history, refunds, excluded transfers/unresolved/future
state, exact boundaries, drivers, attribution, savings, snapshot cashflow
reconciliation, concrete blockers, authentication, and household isolation.

Run API/worker tests and vet against disposable PostgreSQL through the existing
CI matrix. Do not run local builds/tests without the runbook's capacity gate.
