# Product Iteration V3 implementation checklist

Governing plan: [`RICHMOD_PRODUCT_ITERATION_V3.md`](RICHMOD_PRODUCT_ITERATION_V3.md)

Environment: IDR only, `Asia/Jakarta` (GMT+7). This checklist records shipped
evidence by cohesive phase; unchecked items remain intentionally scheduled.

## P0 — Telegram interaction latency

- [x] Conversational LLM input is explicitly delimited as untrusted data,
  restricted to Indonesian/English finance intents, and language-validated before
  deterministic Go processing.
- [x] Telegram text extraction receives a bounded five-minute, 12-message
  same-chat context window for natural follow-ups; historical context remains
  explicitly untrusted.
- [x] Follow-up corrections can deterministically update a uniquely matched
  transaction's date/time (for example, “kemarin” or “sore kemarin”).

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
- [x] Original Telegram callback messages are edited and obsolete inline buttons
  are removed; failed edits use a non-mutating notification fallback.
- [ ] Callback execution latency metrics.
- [x] Reserved concurrent interactive worker loop keeps interactive jobs isolated
  from long-running default/background work (worker build and Telegram tests pass).

## P0 — Household-wide review delivery

- [x] One shared review state fans out to every active linked Telegram identity
  in the household.
- [x] Each recipient has its own bound Telegram message, while any member action
  resolves the same review exactly once.
- [x] Existing review records are backfilled into the recipient mapping.

## Remaining V3 phases

- [x] Multi-image web submissions and Telegram albums are represented as one
  logical document; `document_page` preserves order and attachment provenance.
  Telegram media groups are serialized by household/group, grouped into one
  document, and document processing is debounced for five seconds so all album
  pages can arrive before classification. The classifier, payslip, and receipt
  extractors send all ordered pages in one vision request.
- [x] Universal `review_item` supports transaction, proposal, source-event, and
  document references; existing Telegram review requests link one-to-one to it.
- [x] Telegram review delivery uses centralized deterministic ordering: preferred
  review user first, then active household members, with the household owner as
  the deterministic fallback; all linked household identities still receive it.
- [ ] Payslip salary source, salary events, and dynamic salary cycles.
- [ ] Cycle-aware web, Telegram, and insights behavior.
- [ ] Indonesian copy and chart/UI polish.
- [ ] Invoice flow, known-account hardening, and off-host backup.
