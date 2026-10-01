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
- All amounts remain whole-IDR decimal strings (integral values may drop a
  trailing `.00`, and the API does not promise a fixed NUMERIC scale). Daily
  averages use two decimal places; ratios use four. No financial arithmetic uses
  binary floats.

## Comparison and drivers

Closed cycles compare with up to three preceding completed salary cycles.
Active cycles use the same elapsed-day prefix of completed cycles; a historical
cycle shorter than that prefix is ineligible. `comparison.mode` distinguishes
`FULL_CYCLE` from `ELAPSED_DAYS`. The comparison periods expose their measured
cutoff, including when it is before their full cycle end.
When an active review uses a historical elapsed-day prefix, that cycle's
`cycles[]` entry exposes the same measured cutoff as `comparison.previous`.
Its full exclusive `end` remains unchanged.

Salary-cycle comparison includes the immediately previous closed cycle in full:
`comparison.previousFullCycle` identifies its exact start/exclusive cutoff;
cashflow/category/merchant projections expose `previousFullCycle`,
`deltaVsPreviousFullCycle`, and `relativeDeltaVsPreviousFullCycle`. Current
active amounts are measured-to-date, not a forecast of the final cycle total.
Full-cycle deltas lead the UI; elapsed-day comparisons remain supplemental.
The full measure remains available when that prior cycle is too short for the
equal-day baseline. It never enters eligible history or the prefix median.
Full-only categories remain visible. No prior closed cycle means null full
context; zero or negative baselines mean null ratios, not 100% growth.
Calendar-month analytics remain separate and unchanged.

`median3Available` is true only with three eligible completed cycles. Missing
categories/merchants within an available comparison cycle contribute zero, not
missing history. No history means null deltas and ratios, not an invented zero
baseline. Refunds reduce category and merchant expense in every comparison.

Category changes expose current, previous, median, absolute/relative delta,
share of net expense, and signed contribution to total expense change.
Contribution is null when total change is zero. Relative delta is null for a
nonpositive denominator. A tiny positive denominator retains its exact baseline
and absolute delta alongside the ratio; the API does not assign significance.

Changes sort by absolute full previous-cycle delta when available, otherwise
comparable-prefix delta or current amount without history, then name/ID for
deterministic ties. Merchant drivers are bounded to
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
cycle, and changed Wealth account sets. An identical current/previous observation
produces `WEALTH_SNAPSHOT_UNCHANGED`; movement stays unavailable rather than
implying a zero change. Source-only loose ends are scoped by receipt time when no
transaction/proposal date is available. No arbitrary
"stale after N days" threshold, AI confidence score, or semantic label is added.

## Consistency and consumers

All queries run in one read-only `REPEATABLE READ` transaction. The response
includes `cycle-review-v1` and generation time. It is recomputed, not cached:
historical canonical corrections are visible on the next read. No new schema,
financial mutation, provider call, or production-data repair is involved.

The engine now lives in `apps/reviewdomain/analyticscore`. The API is a thin
authorized adapter; [shared native analytical READ tools](ANALYTICS_SHARED_READ_TOOLS.md)
use the same calculations and explicit model-safe projections. API canonical
IDs remain server/browser-only. The [cycle-review Web UI](ANALYTICS_CYCLE_REVIEW_UI.md)
consumes this response directly. Meeting mode and decision persistence remain
separate subsequent work.

## Verification

`apps/api/internal/analytics/review_test.go` covers median/outlier/tiny-baseline
arithmetic and absence of semantic output fields. `review_integration_test.go`
covers no/one/three-cycle history, refunds, excluded transfers/unresolved/future
state, exact boundaries, drivers, attribution, savings, snapshot cashflow
reconciliation, concrete blockers, authentication, and household isolation.

Run API/worker tests and vet against disposable PostgreSQL through the existing
CI matrix. Do not run local builds/tests without the runbook's capacity gate.

## Legacy analytics consistency — October 1, 2026

Calendar buckets include every overlapping Jakarta month, bounded by the exact
requested start/exclusive end. Short cycles and trailing partial months are not
dropped. `/spending` consumes already-net expense without subtracting refunds
again. `/cycle-daily` exposes net `expense`, separate `grossExpense`, and
refund-adjusted spent/cumulative/remaining values. Salary NUMERIC text is parsed
exactly, accepting decimal-scale input without silent parse-to-zero. Integral
salary/remaining values use whole-IDR strings; fractional values retain cents.
Merchant shares use all confirmed net expense, including undisplayed merchants
and refund-only groups, not the displayed top-ten subtotal.

Legacy current-cycle analytics and Telegram CURRENT/PREVIOUS_CYCLE READs use
Jakarta midnight instants. Legacy `get_category_breakdown` retains signed nonzero
categories, orders by absolute amount, and reports `total_categories`, full
`net_expense_idr`, and `truncated` for its twenty-row limit. No financial records
are rewritten by these fixes.

For `/spending`, legacy `expense` and `netSpending` are both already net;
`refund` is separate gross provenance and must not be subtracted again. Category
and member display joins are household-scoped; invalid foreign bindings retain
amounts in the unnamed group rather than exposing another household's identity.
