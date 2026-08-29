# Analytics Composition Polish — Completion Checklist

Execution record for current analytics visual-hierarchy request. Source prompt remains unchanged.

## Composition

- [x] Keep mode controls first.
- [x] Place KPI cards before deterministic chart.
- [x] Place `Analisis Richmod` after deterministic chart and before details.
- [x] Keep failed, pending, and empty insight states compact.
- [x] Keep successful insight as normal analyst-note card.
- [x] Reduce cycle chart desktop height to 310px and mobile height to 270px.
- [x] Keep daily expense bars and average reference line unchanged.
- [x] Replace ambiguous cycle fraction with `Hari siklus · Hari ke-N`.
- [x] Make category ranking primary lower panel; stack merchant/member at right on desktop.
- [x] Put calendar refund ranking in dedicated full-width row.
- [x] Stack lower analytics panels on tablet/mobile.

## Guardrails

- [x] No insight API, worker, gateway, prompt, financial-state, or aggregation changes.
- [x] No new dependency or TypeScript migration.

## Verification

- [x] Run web tests: `npm test` — 27 passed in Node 22 container.
- [x] Run production build: `npm run build` — passed.
- [x] Inspect diff: frontend composition, insight/card styles, pure helper/tests, and this checklist only.
- [ ] Commit and push feature branch.
- [ ] Do not deploy unless explicitly requested.

## Completion record

```text
Baseline main SHA: 0241ed4c9871f312f105d8059da4cc98f93192d4
Branch: feat/analytics-composition-polish
Worktree: /home/ubuntu/richmod-worktrees/analytics-composition-polish
Backend changes: None.
Cycle KPI conclusion: `daysTotal` can equal elapsed days when next salary anchor is unknown; no denominator is shown.
```
