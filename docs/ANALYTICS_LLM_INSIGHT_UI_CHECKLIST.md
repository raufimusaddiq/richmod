# Analytics LLM Insight UI — Completion Checklist

Execution record for `RICHMOD_ANALYTICS_LLM_INSIGHT_UI_CODEX.md`.
Source plan remains unchanged.

## Scope and safety

- [x] Reuse existing `GET /api/v1/insights` and `POST /api/v1/insights/generate` APIs.
- [x] Keep insight generation background-only; browser never calls LLM provider.
- [x] Keep deterministic charts and KPI values unchanged.
- [x] Render persisted narrative as plain text; no HTML execution surface.
- [x] Preserve aggregate-only backend architecture; no backend/worker changes.

## UI behavior

- [x] Add `Analisis Richmod` after main deterministic chart.
- [x] Match cycle insight by `metrics.period_kind` and active `cycleStart`.
- [x] Suppress generation when no active salary cycle exists, preventing calendar insight mislabeling.
- [x] Do not auto-generate on page load.
- [x] Support no-insight, pending, succeeded, failed, and calendar limitation states.
- [x] Resume bounded polling for a persisted pending insight after page reload.
- [x] Show Indonesian completeness label and percentage.
- [x] Show completed timestamp when available.
- [x] Keep insight failures local to card; charts remain available.
- [x] Use visible generation controls with disabled pending state.
- [x] Bound polling to 15 attempts at 1.8 seconds and cancel on unmount/mode/cycle change.

## Verification

- [x] Run `npm test`: 25 tests passed in Node 22 container.
- [x] Run `npm run build`: production build passed.
- [x] Inspect diff: frontend insight UI, tests, styles, and this checklist only.
- [x] Commit and push feature branch.
- [x] Merge into `main` with `--no-ff` and push.
- [x] Deploy web with production compose files and environment.
- [x] Verify `/`, `/analytics`, `/reviews`, and `/transactions` return HTTP 200.
- [x] Verify `/api/v1/insights` remains session-gated: unauthenticated HTTP 401.

## Completion record

```text
Baseline main SHA: 13669a3328c4d75f28ae2a8cd071dcef25d06924
Branch: feat/analytics-llm-insight-ui
Worktree: /home/ubuntu/richmod-worktrees/analytics-llm-insight-ui
Backend changes: None.
Tests: `npm test` — 25 passed; `npm run build` — passed.
Manual desktop/mobile verification: pending authenticated browser check after deployment.
Implementation commit: fdcb258
Code merge commit: 9daa319
Deployed image: sha256:8d9524d808d697e071fc92d9d2939acf36d2c218e55c1e07800ebd0a2f6177ba
Deployment result: `family-finance-web-1` running; public route checks passed.
```
