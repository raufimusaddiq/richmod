# IR-00 Baseline Re-audit

Baseline re-audited against `main` after PR #144 merged.

- Merged contract: PR #144 -> `b79bd7b`.
- Original audited baseline `ab6ce956ad129eda240f1e88182e01fe2d654606` is an
  ancestor of `b79bd7b`; no task intent moved.
- Re-audited 2026-09-24 against current code; no implementation paths from the
  plan were found missing or already fixed.

## Task to current-path map

| Task | Current paths |
| --- | --- |
| IR-01 residual ReviewDecision correctness | `apps/worker/internal/reviewdec/presets.go`, `apps/worker/internal/document/receipt.go`, `apps/worker/internal/document/screenshot.go`, `apps/worker/internal/document/screenshot_choice.go` |
| IR-02 canonical confirmation guard | `apps/api/internal/review/handler.go`, `apps/api/internal/review/proposal.go`, `apps/worker/internal/telegram/review.go` |
| IR-03 RHICE measurement correctness | `apps/api/internal/operations/product.go`, resolution telemetry writers under `apps/api/internal/review/` |
| IR-04 Telegram single-pass routing | `apps/worker/internal/telegram/transaction_decision.go`, `apps/worker/internal/telegram/judgment_workflows.go`, `apps/worker/internal/telegram/agent*` |
| IR-05 receipt residual bounded rescue | `apps/worker/internal/document/receipt.go`, `apps/worker/internal/reviewdec/presets.go` |
| IR-06 screenshot selective residual batch | `apps/worker/internal/document/screenshot_choice.go`, `apps/worker/internal/document/screenshot.go` |
| IR-07 bank email merchant-less category | `apps/worker/internal/bankemail/policy.go`, `apps/worker/internal/bankemail/processor.go`, `apps/worker/internal/bankemail/decision.go` |
| IR-08 payslip residual-policy separation | `apps/worker/internal/document/payslip.go`, `apps/worker/internal/reviewdec/presets.go` |
| IR-09 intelligence-pass telemetry | `apps/api/internal/operations/product.go`, `apps/worker/internal/telegram/judgment_turn_telemetry.go`, migration dir `db/migrations/` |
| IR-10 regression/canary/rollout | worker/API integration tests, `docs/runbooks/sprint-delivery.md` |

## Notes

- No product requirement was silently dropped because code moved; the merged
  contract is docs-only and the implementation surface is unchanged since the
  audited baseline.
- IR-01 begins implementation on this same merged baseline.
