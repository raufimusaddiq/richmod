# Household cycle-review UI

Sprint 3 of [the execution plan](plans/analytics-cycle-review-sprint.md).
The source contract remains the [cycle-review PRD](RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md).

## Selection and data

- `/analytics?view=cycle&cycle=YYYY-MM-DD` selects one salary cycle.
  Omit `cycle` for the active cycle; the resolved salary start is written to
  the URL. A missing salary anchor displays the server's elapsed-calendar fallback
  and a settings action, without enabling commentary.
- `category=<category-id>` selects deterministic category evidence.
  `category=uncategorized` selects the API's empty-category group.
  Selection survives URL navigation, reload, and browser Back.
- `/analytics?view=calendar&range=3|6|12` retains secondary calendar analysis.
  `from=YYYY-MM&to=YYYY-MM` preserves the existing custom-month range contract.

Cycle review consumes one authoritative `cycle-review-v1` response. It does not
combine calendar aggregates or derive authoritative totals, baselines, averages,
shares, or significance in JavaScript. Browser math formats percentages and maps
server amounts to visual lengths only. Invalid/failed responses show a retry
state, never zero-valued financial facts or another cycle's stale result.

## Document hierarchy

1. Explicit period, state, measured cutoff and Jakarta timezone.
2. Cycle outcome: dominant net cashflow, income, refund-adjusted expense,
   savings allocated, unallocated surplus.
3. Daily spending shape: retained daily bars and server-owned average reference,
   peak, peak share, zero-spend days, plus accessible daily values.
4. Category changes: current, previous, median of three eligible cycles,
   signed absolute/relative deltas against both baselines, contribution to total
   change, proportional visual weight. No significance labels.
5. Selected-category drivers: bounded merchant comparisons, supporting
   expense/refund transactions, category share and count.
6. Category distribution and whole-cycle merchant drivers.
7. Descriptive household attribution, never member responsibility rankings.
8. Savings destinations and Wealth observations/reconciliation. Snapshot links,
   timestamps and server observation ages remain explicit. Missing reconciliation
   stays missing; no guessed cycle-end balance, investment P/L or stale threshold.
9. Concrete quality blockers and Inbox, transaction, settings or Wealth actions.
10. Optional non-authoritative cycle commentary alongside supporting-data links.

Focused meeting mode and explicitly persisted household decisions remain Sprint 4,
not a fabricated browser-only decision log in this sprint.

## Drill-down

Transaction links carry `from`, inclusive `to`, `status=CONFIRMED`,
`type=SPENDING`, cycle start, and applicable category/merchant/transaction ID.
The UI converts the facts API's exclusive cutoff to the ledger's inclusive date
filter, so the next cycle's Jakarta midnight is excluded.

The household-scoped ledger list adds:

- `type=SPENDING`: confirmed status is a separate filter; type selects
  `EXPENSE` and `REFUND`, never income or transfers.
- `merchantId=<uuid>`: validated exact canonical merchant binding, not a
  substring name search.
- `categoryId=uncategorized`: null category binding.
- `id=<uuid>`: exact household-scoped evidence transaction binding, also used
  by the existing detail drawer.

Existing transaction filters, pagination and authorization remain intact.
The ledger preserves the cycle context and offers a return link; merchant
filters can be explicitly cleared. All of this is read-only.

## Commentary isolation

Only non-historical commentary matching the selected salary-cycle start is shown.
Both `CURRENT_CYCLE` and `SALARY_CYCLE` metadata are supported.
Historical advice rows remain unchanged in PostgreSQL but are not presented as
current cycle-review commentary.

Generation requires an explicit user action and passes the selected start to
the existing tool-first endpoint. Loading the review or switching categories
does not request a model turn. Provider failure leaves charts, comparisons,
drivers, savings, Wealth and quality actions intact. No confidence/quality score,
recommendation DTO or prose-to-finance parsing is added. The existing server
completeness gate remains authoritative.

## Verification

- `npm test`: formatting/null baselines, date boundaries, URL selection,
  historical commentary exclusion, deterministic rendering boundaries.
- API integration tests: exact merchant matching, refunds, inclusive cutoff
  translation, uncategorized spending, household isolation.
- CI web build and `scripts/analytics-review-smoke.mjs`: synthetic active and
  closed cycles, median/outlier context, keyboard category selection, correct
  drill-down filters, browser Back, calendar URL state, provider outage, failed
  facts, missing salary anchor, no automatic generation, no page overflow.
- Screenshots use only synthetic fixtures at 1440, 1024, 390 and 320px widths;
  CI publishes them as `cycle-review-screenshots` for visual inspection.

No schema migration, provider change, deployment, restart or production DML.
