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
- [x] Matching expense/date follow-ups first create a chat-bound pending edit;
  only `yes/ya` applies the update, while `no/tidak` cancels it.
- [x] Native gateway `function_call` protocol is available and parses strict
  tool calls; Go-side registered tool dispatch and tool-result round trips are
  available for finance actions.
- [x] Go-owned native finance tool catalog and strict argument validator define
  query, create, propose-edit, confirm-edit, and cancel-edit tools.
- [x] Native `function_call_output` round-trip support returns the next tool call
  or final assistant text without granting the model direct database access.
- [x] Native query and propose-edit calls are wired to bounded Go handlers;
  edits remain confirmation-gated before mutation, and native confirm/cancel
  calls dispatch through the same guarded handlers.
- [x] Native tool calls now exchange bounded Go-produced function results in a
  maximum four-step loop until the gateway returns no further tool call.
- [x] Multi-transaction messages support up to ten IDR entries, stage one
  household/chat-bound pending batch, and require a single yes/ya confirmation
  before atomically recording all entries with individual evidence and audit
  links.

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
- [x] Callback webhook latency is emitted as redacted structured logs; queue
  integrity now rejects successful Telegram jobs whose source event remains
  RECEIVED/PROCESSING, making orphaned events retryable.
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
- [~] Payslip validation now persists IDR salary sources/events for confirmed
  payslips, deduplicates by source and payroll period, and provides a Jakarta
  dynamic-cycle resolver. Payslip images are accepted through both Telegram
  (including albums) and the web multi-image upload pipeline. Explicit
  Indonesian/English Telegram caption dates (for example, “gajian tanggal 24
  agustus 2026”) deterministically set pay_date before confirmation. Interactive
  salary-source selection and cycle-aware consumers remain in the next slice.
- [x] Confirming a document-backed Telegram review atomically marks the linked
  document `EXTRACTED`, preventing stale `NEEDS_REVIEW` document records.
- [~] Cycle-aware web, Telegram, and insights behavior. Ringkasan and Analisis now expose the active cycle and daily current-cycle cashflow; Telegram and insight consumers remain scheduled.
  the resolved active salary cycle (or calendar fallback); Telegram and
  insight consumers remain scheduled.
- [~] Cycle-aware foundation shipped: authenticated `GET /api/v1/analytics/cycle`
  resolves the current confirmed primary-salary cycle in Asia/Jakarta and
  explicitly falls back to the calendar month when salary setup is absent.
- [ ] Indonesian copy and chart/UI polish.
- [ ] Invoice flow, known-account hardening, and off-host backup.
