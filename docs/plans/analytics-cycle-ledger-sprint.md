# Analytics Cycle Ledger — Sprint Plan

**Status:** implementation execution plan — 2026-10-02
**Source contract:** `docs/RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md` (§13.1), `docs/bdr/BDR-006-analytics-household-cycle-review.md` (amendment 2026-10-02)
**Design mock:** `docs/assets/analytics-cycle-ledger-mock.html` (static, synthetic numbers; open in a browser)
**Drift gate:** `docs/ANALYTICS_CYCLE_REVIEW_DRIFT_GUARD_CHECKLIST.md`
**Builds on:** `docs/plans/analytics-cycle-review-sprint.md` (Sprints 1–4 shipped the facts API, the scan-first report and meeting mode)

## 0. How to execute this plan

One branch, linked worktree and pull request per item below, strictly in order.
The items share `apps/web/app/analytics/page.js`, and the build host is small, so
nothing runs in parallel.

For every item:

1. read root `AGENTS.md`, `docs/runbooks/sprint-delivery.md`, the PRD, BDR-006 and the drift guard;
2. run the runbook's host capacity gate (`free -m`, `df -h /`, `uptime`) before any build or test;
3. create the worktree from current `main`:
   `git worktree add -b <type>/<name> ../family-finance-worktrees/<name> main`;
4. implement only that item's scope and update the named docs in the same branch;
5. run the smallest relevant local checks; leave DB integration tests, `next build`
   and Playwright baselines to CI unless the capacity gate passes;
6. commit, push, open a PR to `main`, wait for CI and Hermes Review, merge with a merge commit;
7. no deployment is part of this sprint.

## 1. Problem

The current `/analytics` cycle view answers "what changed" for one cycle against
one baseline, in labels and tables. The household cannot see several cycles side
by side, which is BDR-006 question 4 ("is the change unusual against recent
history?"). Audit findings that drive this plan:

- The server already computes `comparison.income` and `comparison.netCashflow`; the page renders neither.
- Daily `cumulativeExpense` was served and unused (PR 2 now plots it as a separate pace chart); the daily chart has no pace reference and labels days by day-of-month only.
- `cycles[]` carries boundaries only, so the browser has no per-cycle totals.
- The active cycle leads with net cashflow, which is misleading mid-cycle because income lands on day 1.
- Most comparison content is text, context spans and disclosures; the PRD lists "long wall of prose" as a thing not to build.
- Switching to Calendar and back drops the reviewed cycle; cycle changes blank the page behind a generic skeleton.

## 2. Decision: the cycle ledger

The primary visual is one figure in which each salary cycle is a column and
everything below the cycle header lines up under it:

```text
period + state + Previous/Next
verdict (label : value pairs, no sentences)
┌ ledger figure ──────────────────────────────────────────────┐
│ cycle columns (select)                                      │
│ income | expense bars, direct value labels, median tick     │
│ net cashflow                                                │
│ expense change vs the previous column                       │
│ category x cycle matrix (top 8 + "Lainnya")                 │
└─────────────────────────────────────────────────────────────┘
pace: cumulative expense, this cycle vs previous vs median
evidence · savings & Wealth · follow-ups · full detail · decisions (existing sections)
```

Ribbon and matrix are one grid (one `<table>` in the mock), so columns align by
construction. The selected cycle is highlighted with `--butter`; a hatched
expense bar marks a running cycle.

### Invariants (carried over, not relaxed)

- Every amount, delta, median, share and total is computed in Go and served. The
  browser only formats numbers and maps amounts to lengths.
- No scores, budgets, significance labels, "unusual" flags or good/bad colouring.
  Direction is a sign plus ▲/▼ in neutral ink. Matrix tint encodes size relative
  to the row's own maximum, as a visual length only.
- The verdict is label–value pairs from served fields. Go and the browser never
  generate interpretive sentences (BDR-006 "Anti-Go semantic boundary").
- Missing history stays `null`/"—". No invented zeros.
- Accent colour stays reserved for interaction; chart colour uses the existing
  `--chart-*`, `--income`, `--expense` and `--info` tokens.
- The page remains fully useful with AI unavailable. Meeting mode and decisions are unchanged.

### States

| State | Behaviour |
| --- | --- |
| Closed cycle selected | Verdict: expense, Δ vs previous full cycle, Δ vs median 3 (all served). Median tick drawn in the selected column. |
| Active cycle selected | Lead with pace. Verdict uses the served equal-day values (`previous`, `deltaVsPrevious`). No partial-vs-full median tick or delta in the column. Pace chart marks full-cycle lines distinctly. |
| Fewer than 4 closed cycles | Show per-cycle stat cards instead of a chart. Matrix still renders. |
| No salary anchor | Existing calendar-month fallback and settings action; ledger hidden. |
| Selected cycle outside the default window | Window ends at the selected cycle so it is always visible. |
| Narrow screens | Label column is narrow and sticky; about 3 columns visible; the selected column scrolls into view on load and on selection. |

### Accessibility

Columns are real column headers with `aria-pressed` selection buttons. Every
number is text; charts have a data-table equivalent. The scroll region is
focusable and labelled. Selection motion (one sliding highlight) respects
`prefers-reduced-motion`. Targets are at least 44px tall.

## 3. API contract added in PR 1 (additive)

`GET /api/v1/analytics/cycle-review?cycle_start=YYYY-MM-DD&history=N`

`history` is an optional integer, default 6, 1–12; anything else is `400`.
The response stays `cycle-review-v1`; new fields are additive and the web client
tolerates their absence.

```text
history[]                      ascending by start; the window is the N most recent
                               cycles, or ends at the selected cycle if it is older
  start, end|null, measuredUntil, state
  income, grossExpense, refund, expense, netCashflow, savingsAllocated   (strings)
categoryHistory
  cycleStarts[]                the same starts as history[], same order
  rows[]                       top 8 categories by net expense summed over the window
    id, name, amounts[]        one amount string per cycle, "0" when none
  other.amounts[]              everything outside the top 8
```

Rules:

- For every cycle, `sum(rows[].amounts[i]) + other.amounts[i] == history[i].expense` exactly (refund-adjusted, categories net).
- The active cycle entry is measured to `measuredUntil`; equal-day comparisons stay in `comparison`.
- Median, previous and delta values for the selected cycle stay in `comparison` and `categoryChanges`. The browser computes none.
- The AI tools in `analyticscore/tools.go` build output from allowlisted fields. `history` and `categoryHistory` must not appear in tool output; a test asserts this.
- No schema change: the history reuses the existing array-parameterised measures query.

## 4. Sequence

### PR 0 — Spec and mock (`docs/analytics-cycle-ledger-spec`)

Docs only: this plan, the mock, PRD §13.1, BDR-006 amendment, a pointer in
`ANALYTICS_CYCLE_REVIEW_UI.md`. No application code.
*Done when:* docs are consistent with each other and CI passes.

### PR 1 — History series (`feat/analytics-cycle-history`)

Go: `analyticscore/facts.go`, `query.go`, `api/internal/analytics/review.go`.
Docs: `ANALYTICS_CYCLE_REVIEW_API.md`.
*Acceptance:* the section 3 contract; reconciliation holds for 0, 1, 3 and 12 cycles, refunds, refund-only categories and a negative "Lainnya"; invalid `history` is `400`; tool output excludes the new fields.
*Tests:* unit tests in `facts_test.go`; integration test in `review_integration_test.go` (CI or disposable Postgres after the capacity gate).

### PR 2 — Ribbon, pace and navigation (`feat/analytics-cycle-ribbon`)

Web: split `page.js` into modules; new ribbon and pace components; `lib/cycleReview.js`; `globals.css`.
*Acceptance:*
- ribbon from `history`, direct value labels, selected column highlight, running-cycle marking, stat-card fallback under 4 closed cycles;
- pace chart: cumulative lines on a day 1…N axis with month ticks, direct end labels, light gridlines with values;
- verdict label–value pairs; active cycle leads with pace;
- Previous/Next buttons; the cycle survives a Calendar toggle; stale-while-revalidate loading;
- the unused served income and net comparisons are rendered;
- plain-language labels; page title follows the view; whole-rupiah daily average; today marked partial.

*Tests:* pure-function tests in `tests/cycle-review.test.mjs`; fixtures gain `history`, `comparison.income` and `comparison.netCashflow`; `ui-audit-locks` forbids colour-only direction; update `scripts/analytics-review-smoke.mjs`.

### PR 3 — Matrix and text demotion (`feat/analytics-cycle-matrix`)

Web only. Category x cycle matrix on the same grid as the ribbon; row click opens the existing category evidence; `ComparisonContext` and `ChangesTable` move under "Detail penuh" with no fact removed; mobile snap-scrolling shared grid.
*Acceptance:* every cell is text; matrix rows reconcile to the ribbon expense; keyboard reachable.

### PR 4 — Polish (`chore/analytics-ledger-polish`)

Web and docs. Colour semantics against `brand-guidelines.md`, weight of the key comparison numbers, collapsed-section labels that say what is inside, remove the CSS-generated table caption string, refresh the stale visual baselines (CI or capacity-gated), update `ANALYTICS_CYCLE_REVIEW_UI.md`, run the drift guard.

### PR 5 — Calendar hygiene (`fix/analytics-calendar-hygiene`) — lowest priority

Web only: surface the API error text, controlled custom-range inputs with min/max, formatted months, drop the duplicate `/spending` request from the calendar view, mark the current month as partial. No API change.

## 4a. Deviations recorded during execution

- **PR 1** shipped as planned (`history` and `categoryHistory`).
- **PR 2** also adds `expenseDelta` to each `history[]` entry (Go, with tests and
  the API doc): the ledger's change row is arithmetic, so it is served rather than
  subtracted in the browser.
- **PR 2** adds new modules (`CycleLedger`, `lib/cycleLedger.js`, `CyclePaceChart`)
  but does not split the existing `page.js`; several tests and the smoke script read
  it directly. The split can follow when the tables move in PR 3.
- **PR 2** draws the pace chart as the served running total plus served comparison
  references (dashed levels, equal-day markers). Previous-cycle and median daily
  curves are not served, so they are not drawn.
- **PR 2** shows income and net comparison through the ledger rows (from `history`)
  rather than a separate use of `comparison.income` / `comparison.netCashflow`.
- **PR 2** plots the served `cumulativeExpense` as the pace chart. If any daily row lacks it, the chart says the total is unavailable instead of disappearing.
- Plain-language relabelling of the existing comparison tables ("Δ", "hari setara",
  "median 3") moves to PR 3, where those tables are demoted, so the smoke script's
  text assertions change in one place.

- **PR 3** puts the matrix rows in the ribbon's table (standalone table under the cards when there are fewer than four closed cycles). A row is clickable only when `categoryChanges` serves that category, because older window categories may have no evidence. The ledger shows in the position and changes meeting steps. The plain-language relabelling shipped here. Scroll-snap was not added; the figure scrolls horizontally and centres the selected column.
- **PR 3** turns "Apa yang berubah?" into the closed "Detail perubahan" disclosure instead of wrapping its contents in another "Detail penuh"; everything it held is still there.

## 5. Verification and resources

- Local: `node --test tests/*.test.mjs` in `apps/web`; Go tests that do not need Postgres.
- CI: DB integration tests, `next build`, Playwright visual baselines.
- Never build inside `apps/web` of the root worktree (`.next` is root-owned); use the item's worktree.
- Use fixtures only; no production household data.

## 6. Non-goals

Deployment; calendar redesign; budgets, targets, scores or significance labels;
AI changes; new infrastructure; schema changes.

## 7. Definition of done

All items merged to `main` with CI and Hermes Review green; PRD, BDR, API and UI
docs match behaviour; the drift-guard checklist passes for each item; the
cycle view lets a household compare six cycles, spot a category change against
recent history and select any cycle from the ribbon without reading prose.
