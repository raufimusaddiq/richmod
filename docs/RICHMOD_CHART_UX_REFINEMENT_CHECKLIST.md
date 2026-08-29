# Richmod Chart UX Refinement — Implementation Checklist

This checklist tracks implementation of
[`RICHMOD_CHART_UX_REFINEMENT_CODEX.md`](RICHMOD_CHART_UX_REFINEMENT_CODEX.md).

The Codex plan remains the source of truth for product intent, scope, design
details, and acceptance criteria. This file is an execution and verification
record only. Do not update the source plan to mark progress.

## Scope and guardrails

- [ ] Confirm the implementation targets the current `main` baseline.
- [ ] Read `AGENTS.md` and follow the dedicated worktree/branch workflow.
- [ ] Keep the work limited to frontend chart UX and related tests/styles.
- [ ] Do not add a chart library, backend endpoint, infrastructure, LLM call, or state-management library.
- [ ] Keep financial meaning descriptive; do not introduce budgets, targets, forecasts, alerts, or investment analytics.
- [ ] Preserve `role="img"`, `aria-label`, localized copy, IDR formatting, and existing page behavior outside this refinement.

## Preflight data-contract checks

- [ ] Verify whether `/api/v1/analytics/cycle/daily` is dense (every elapsed date) or sparse (activity dates only).
- [ ] Record the daily-series contract used for zero-spend-day metrics.
- [ ] Confirm calendar cashflow rows contain `period`, `income`, `expense`, and `netCashflow`.
- [ ] Confirm category rows expose stable `id`, `name`, and numeric/string-safe `amount` values.
- [ ] Resolve any contract ambiguity before implementing zero-spend metrics; do not infer missing dates from screenshots or transaction rows.

## C1 — Ringkasan daily spending preview

- [ ] Add `DashboardDailySpendingChart`.
- [ ] Use daily expense bars only.
- [ ] Remove income, net, cumulative, dual-axis, and log-scale visuals from the Ringkasan hero.
- [ ] Keep income, expense, net, and review status in the existing KPI cards.
- [ ] Make the eyebrow period-aware: salary cycle versus calendar fallback.
- [ ] Use the existing daily analytics data without adding an API call.
- [ ] Preserve zero-expense days in the displayed series when the API contract supports them.
- [ ] Add useful empty/loading behavior.
- [ ] Verify daily tooltip date and IDR formatting.
- [ ] Verify desktop and mobile readability.

## C2 — Analisis / Siklus Gaji spending pattern

- [ ] Add `CycleSpendingPatternChart`.
- [ ] Render daily expense bars with an average-spending reference line.
- [ ] Remove the cumulative expense line and unnecessary single-series legend.
- [ ] Add and test `deriveCycleSpendingMetrics`.
- [ ] Define average as total spent divided by `max(daysElapsed, 1)`.
- [ ] Derive the correct peak day.
- [ ] Count zero-spend days according to the verified daily-series contract.
- [ ] Handle zero elapsed days, one-day cycles, and all-zero data without `NaN`.
- [ ] Replace cycle-mode KPIs with average/day, peak day, zero-spend days, and cycle progress.
- [ ] Remove misleading monthly-spending presentation from cycle mode.
- [ ] Keep category, merchant, and member analysis below the cycle hero.
- [ ] Verify cycle tooltip and reference-line label remain readable on mobile.

## C3 — Analisis / Kalender monthly comparison

- [ ] Add `MonthlyCashflowChart`.
- [ ] Map monthly rows through a pure helper with safe numeric conversion and month labels.
- [ ] Render grouped income and expense bars, not stacked bars.
- [ ] Keep net cashflow in the tooltip and/or calendar KPI cards, not as a third visual series.
- [ ] Use calendar-specific KPIs: total income, total expense, net cashflow, and refunds.
- [ ] Preserve 3/6/12-month filters.
- [ ] Preserve custom date-range behavior.
- [ ] Verify empty calendar data renders a clear empty state.
- [ ] Verify monthly tooltip labels all series and net cashflow in localized IDR.

## C4 — Category visualizations

### Ringkasan donut

- [ ] Add or repurpose `CategoryDonutChart`.
- [ ] Add and test `compactCategories`.
- [ ] Filter non-positive categories before compaction.
- [ ] Render Top 5 plus `Lainnya`, for a maximum of six slices.
- [ ] Use the exact same compact dataset for the donut and legend.
- [ ] Verify `Lainnya` equals the omitted category total.
- [ ] Verify compact shares sum approximately to 1.

### Analisis ranking

- [ ] Add `CategoryRankingChart`.
- [ ] Use horizontal bars sorted by descending amount.
- [ ] Show top 8 or all categories when the list is small.
- [ ] Use `Lainnya` when additional categories are compacted.
- [ ] Expose amount and share through tooltip/label.
- [ ] Ensure every visual category has a matching readable label.

## C5 — Component and composition cleanup

- [ ] Replace all page imports/usages of `CashflowChart`, `DailyCashflowChart`, and `CategoryChart`.
- [ ] Confirm the final semantic component set is:
  - [ ] `DashboardDailySpendingChart`
  - [ ] `CycleSpendingPatternChart`
  - [ ] `MonthlyCashflowChart`
  - [ ] `CategoryDonutChart`
  - [ ] `CategoryRankingChart`
- [ ] Remove unused old components or fully repurpose them with no duplicate implementation.
- [ ] Remove unused Recharts imports, area/line code, and gradient definitions.
- [ ] Keep pure derivation helpers in `apps/web/app/lib/chartData.js` or the repository’s equivalent.
- [ ] Keep page components responsible for fetching and composition, not financial calculations.
- [ ] Preserve responsive layout without unrelated page redesign.

## Visual and accessibility verification

- [ ] Ringkasan answers “kapan uang keluar selama periode aktif?” without salary-scale distortion.
- [ ] Siklus Gaji answers “hari mana pengeluaran tinggi atau rendah sejak gajian?”.
- [ ] Kalender answers “bagaimana pemasukan dan pengeluaran berubah antar bulan?”.
- [ ] Income uses the existing positive/green visual language.
- [ ] Spending uses the existing coral/red visual language.
- [ ] Reference lines use muted blue and remain descriptive, not prescriptive.
- [ ] No 3D, dual Y-axis, log scale, excessive gradients, or animated gimmicks are introduced.
- [ ] Empty states distinguish no data from zero spending.
- [ ] All chart containers retain `role="img"` and meaningful `aria-label` values.
- [ ] Monthly comparison meaning is available through labels/tooltips and is not color-only.
- [ ] Verify wide desktop, tablet, and mobile layouts.
- [ ] Verify one day, six days, 31-day cycle, zero-spend days, one large expense, salary much larger than expenses, more than six categories, 3/12 months, and empty states.
- [ ] Check x-axis ticks, tooltip clipping, reference labels, legends, card height, category labels, and horizontal overflow.

## Automated tests

- [ ] Add `apps/web/tests/chart-data.test.mjs` or the repository-equivalent pure-helper test file.
- [ ] Test category compaction for 0, 1, 5, 6, and 10 categories.
- [ ] Test Top 5 preservation, `Lainnya` total, and share calculation.
- [ ] Test cycle average, zero elapsed days, peak day, zero-spend days, and all-zero cycle.
- [ ] Test monthly numeric conversion, month labels, and empty data.
- [ ] Update product-alignment tests without brittle JSX-whitespace assumptions.
- [ ] Run `cd apps/web && npm test`.
- [ ] Run `cd apps/web && npm run build`.

## Change review and handoff

- [ ] Inspect the final diff for unrelated backend, database, or infrastructure changes.
- [ ] Confirm no API changes were introduced unless separately justified and documented.
- [ ] Confirm the base plan `RICHMOD_CHART_UX_REFINEMENT_CODEX.md` was not edited.
- [ ] Record baseline `main` SHA, branch, worktree, changed files, and component changes.
- [ ] Commit with a descriptive message.
- [ ] Push the feature branch.
- [ ] Merge the feature branch into `main` with `--no-ff`.
- [ ] Push updated `main`.
- [ ] Do not deploy unless explicitly requested.

## Completion record

```text
Baseline main SHA:
Branch:
Worktree:
Files changed:
Components added/removed:
API changes:
Tests and results:
Build result:
Responsive verification:
Commit SHA:
Merge SHA:
Pushed main SHA:
Deployment result:
Remaining caveats:
```
