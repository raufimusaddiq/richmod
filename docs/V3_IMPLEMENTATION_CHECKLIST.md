# Product Iteration V3 implementation checklist

Governing plan: [`RICHMOD_PRODUCT_ITERATION_V3.md`](RICHMOD_PRODUCT_ITERATION_V3.md)

Environment: IDR only, `Asia/Jakarta` (GMT+7). This checklist records shipped
evidence by cohesive phase; unchecked items remain intentionally scheduled.

## P0 — Telegram interaction latency

- [x] Inline callbacks are persisted as `TELEGRAM_CALLBACK` source events.
- [x] Callback persistence is idempotent by Telegram update external ID.
- [x] Callback actions are queued as `PROCESS_TELEGRAM_CALLBACK`.
- [x] Callback jobs use the dedicated `INTERACTIVE` queue lane.
- [x] Webhook sends `answerCallbackQuery` after durable capture and before HTTP 204.
- [x] Callback processing reuses deterministic review handling and does not invoke
  the general text/LLM pipeline for stale or resolved actions.
- [x] Existing text, image, Gmail, document, and insight jobs retain explicit
  `DEFAULT`/`BACKGROUND` lanes.
- [x] API and worker production images compile successfully with Docker builds.
- [ ] Original Telegram message edit APIs and callback execution latency metrics.
- [ ] Reserved concurrent interactive worker loop and end-to-end Telegram latency test.

## Remaining V3 phases

- [ ] Multi-image logical documents and Telegram album finalization.
- [ ] Universal review item and deterministic recipient routing.
- [ ] Payslip salary source, salary events, and dynamic salary cycles.
- [ ] Cycle-aware web, Telegram, and insights behavior.
- [ ] Indonesian copy and chart/UI polish.
- [ ] Invoice flow, known-account hardening, and off-host backup.
