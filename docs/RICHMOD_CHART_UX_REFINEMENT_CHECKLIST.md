# Richmod Chart UX Refinement — Implementation Checklist

This checklist tracks implementation of
[`RICHMOD_CHART_UX_REFINEMENT_CODEX.md`](RICHMOD_CHART_UX_REFINEMENT_CODEX.md).

The Codex plan remains the source of truth for product intent, scope, design
details, and acceptance criteria. This file is an execution and verification
record only. Do not update the source plan to mark progress.

## Scope and guardrails

- [x] Confirm the implementation targets the current `main` baseline.
- [x] Read `AGENTS.md` and follow the dedicated worktree/branch workflow.
- [x] Keep the work limited to frontend chart UX and related tests/styles.
- [x] Do not add a chart library, backend endpoint, infrastructure, LLM call, or state-management library.
- [x] Keep financial meaning descriptive; do not introduce budgets, targets, forecasts, alerts, or investment analytics.
- [x] Preserve `role="img"`, `aria-label`, localized copy, IDR formatting, and existing page behavior outside this refinement.

## Preflight data-contract checks

- [x] Verify whether `/api/v1/analytics/cycle/daily` is dense (every elapsed date) or sparse (activity dates only).
- [x] Record the daily-series contract used for zero-spend-day metrics.
- [x] Confirm calendar cashflow rows contain `period`, `income`, `expense`, and `netCashflow`.
- [x] Confirm category rows expose stable `id`, `name`, and numeric/string-safe `amount` values.
- [x] Resolve any contract ambiguity before implementing zero-spend metrics; do not infer missing dates from screenshots or transaction rows.

## C1 — Ringkasan daily spending preview

- [x] Add `DashboardDailySpendingChart`.
- [x] Use daily expense bars only.
- [x] Remove income, net, cumulative, dual-axis, and log-scale visuals from the Ringkasan hero.
- [x] Keep income, expense, net, and review status in the existing KPI cards.
- [x] Make the eyebrow period-aware: salary cycle versus calendar fallback.
- [x] Use the existing daily analytics data without adding an API call.
- [x] Preserve zero-expense days in the displayed series when the API contract supports them.
- [x] Add useful empty/loading behavior.
- [x] Verify daily tooltip date and IDR formatting.
- [ ] Verify desktop and mobile readability (requires browser visual inspection).

## C2 — Analisis / Siklus Gaji spending pattern

- [x] Add `CycleSpendingPatternChart`.
- [x] Render daily expense bars with an average-spending reference line.
- [x] Remove the cumulative expense line and unnecessary single-series legend.
- [x] Add and test `deriveCycleSpendingMetrics`.
- [x] Define average as total spent divided by `max(daysElapsed, 1)`.
- [x] Derive the correct peak day.
- [x] Count zero-spend days according to the verified daily-series contract.
- [x] Handle zero elapsed days, one-day cycles, and all-zero data without `NaN`.
- [x] Replace cycle-mode KPIs with average/day, peak day, zero-spend days, and cycle progress.
- [x] Remove misleading monthly-spending presentation from cycle mode.
- [x] Keep category, merchant, and member analysis below the cycle hero.
- [ ] Verify cycle tooltip and reference-line label remain readable on mobile (requires browser visual inspection).

## C3 — Analisis / Kalender monthly comparison

- [x] Add `MonthlyCashflowChart`.
- [x] Map monthly rows through a pure helper with safe numeric conversion and month labels.
- [x] Render grouped income and expense bars, not stacked bars.
- [x] Keep net cashflow in the tooltip and/or calendar KPI cards, not as a third visual series.
- [x] Use calendar-specific KPIs: total income, total expense, net cashflow, and refunds.
- [x] Preserve 3/6/12-month filters.
- [x] Preserve custom date-range behavior.
- [x] Verify empty calendar data renders a clear empty state.
- [x] Verify monthly tooltip labels all series and net cashflow in localized IDR.

## C4 — Category visualizations

### Ringkasan donut

- [x] Add or repurpose `CategoryDonutChart`.
- [x] Add and test `compactCategories`.
- [x] Filter non-positive categories before compaction.
- [x] Render Top 5 plus `Lainnya`, for a maximum of six slices.
- [x] Use the exact same compact dataset for the donut and legend.
- [x] Verify `Lainnya` equals the omitted category total.
- [x] Verify compact shares sum approximately to 1.

### Analisis ranking

- [x] Add `CategoryRankingChart`.
- [x] Use horizontal bars sorted by descending amount.
- [x] Show top 8 or all categories when the list is small.
- [x] Use `Lainnya` when additional categories are compacted.
- [x] Expose amount and share through tooltip/label.
- [x] Ensure every visual category has a matching readable label.

## C5 — Component and composition cleanup

- [x] Replace all page imports/usages of `CashflowChart`, `DailyCashflowChart`, and `CategoryChart`.
- [x] Confirm the final semantic component set is:
  - [x] `DashboardDailySpendingChart`
  - [x] `CycleSpendingPatternChart`
  - [x] `MonthlyCashflowChart`
  - [x] `CategoryDonutChart`
  - [x] `CategoryRankingChart`
- [x] Remove unused old components or fully repurpose them with no duplicate implementation.
- [x] Remove unused Recharts imports, area/line code, and gradient definitions.
- [x] Keep pure derivation helpers in `apps/web/app/lib/chartData.js` or the repository’s equivalent.
- [x] Keep page components responsible for fetching and composition, not financial calculations.
- [x] Preserve responsive layout without unrelated page redesign.

## Visual and accessibility verification

- [x] Ringkasan answers “kapan uang keluar selama periode aktif?” without salary-scale distortion.
- [x] Siklus Gaji answers “hari mana pengeluaran tinggi atau rendah sejak gajian?”.
- [x] Kalender answers “bagaimana pemasukan dan pengeluaran berubah antar bulan?”.
- [x] Income uses the existing positive/green visual language.
- [x] Spending uses the existing coral/red visual language.
- [x] Reference lines use muted blue and remain descriptive, not prescriptive.
- [x] No 3D, dual Y-axis, log scale, excessive gradients, or animated gimmicks are introduced.
- [x] Empty states distinguish no data from zero spending.
- [x] All chart containers retain `role="img"` and meaningful `aria-label` values.
- [x] Monthly comparison meaning is available through labels/tooltips and is not color-only.
- [ ] Verify wide desktop, tablet, and mobile layouts (requires browser visual inspection).
- [ ] Verify one day, six days, 31-day cycle, zero-spend days, one large expense, salary much larger than expenses, more than six categories, 3/12 months, and empty states (requires browser visual inspection).
- [ ] Check x-axis ticks, tooltip clipping, reference labels, legends, card height, category labels, and horizontal overflow (requires browser visual inspection).

## Automated tests

- [x] Add `apps/web/tests/chart-data.test.mjs` or the repository-equivalent pure-helper test file.
- [x] Test category compaction for 0, 1, 5, 6, and 10 categories.
- [x] Test Top 5 preservation, `Lainnya` total, and share calculation.
- [x] Test cycle average, zero elapsed days, peak day, zero-spend days, and all-zero cycle.
- [x] Test monthly numeric conversion, month labels, and empty data.
- [x] Update product-alignment tests without brittle JSX-whitespace assumptions.
- [x] Run `cd apps/web && npm test` in a Node 22 container (18 tests pass).
- [x] Run `cd apps/web && npm run build` in a Node 22 container (production build passes).

## Change review and handoff

- [x] Inspect the final diff for unrelated backend, database, or infrastructure changes.
- [x] Confirm no API changes were introduced unless separately justified and documented.
- [x] Confirm the base plan `RICHMOD_CHART_UX_REFINEMENT_CODEX.md` was not edited.
- [x] Record baseline `main` SHA, branch, worktree, changed files, and component changes.
- [x] Commit with a descriptive message.
- [x] Push the feature branch.
- [x] Merge the feature branch into `main` with `--no-ff`.
- [x] Push updated `main`.
- [x] Do not deploy unless explicitly requested.

## Completion record

```text
Baseline main SHA:
`ef55807`
Branch: `feat/chart-ux-refinement`
Worktree: `/home/ubuntu/family-finance-worktrees/chart-ux-refinement`
Files changed: `apps/web/app/page.js`, `apps/web/app/analytics/page.js`, `apps/web/app/components/Charts.js`, `apps/web/app/lib/chartData.js`, `apps/web/app/globals.css`, `apps/web/tests/chart-data.test.mjs`, `apps/web/tests/product-alignment.test.mjs`, this checklist
Components added/removed: Added five semantic chart components; removed the three generic chart implementations.
API changes: None.
Tests and results: `npm test` — 18 passed.
Build result: `npm run build` — passed.
Responsive verification: Code/CSS review completed; browser visual inspection remains pending.
Commit SHA: `733b1ad`
Merge SHA: `7bcf569`
Pushed main SHA: `7bcf569`
Deployment result: Not requested; not deployed.
Remaining caveats: Three browser-only visual inspection items remain unchecked.
```
