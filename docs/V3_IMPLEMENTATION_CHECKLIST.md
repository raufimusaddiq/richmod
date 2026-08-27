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
  agustus 2026”) deterministically set pay_date before confirmation. Web owners
  can select the household's primary salary source. Missing-pay-date review
  replies now accept an explicit Indonesian/English date and create the linked
  salary event transactionally; Pengaturan now shows the active source plus
  primary/backup state and keeps selection owner-only.
- [x] Confirming a document-backed Telegram review atomically marks the linked
  document `EXTRACTED`, preventing stale `NEEDS_REVIEW` document records.
- [~] Cycle-aware web, Telegram, and insights behavior. Ringkasan and Analisis
  show salary baseline, spent, remaining, elapsed days, daily expense bars, and
  cumulative spend. Telegram resolves `CURRENT_CYCLE` and `PREVIOUS_CYCLE` from
  confirmed primary salary anchors. Insights accept `period=cycle` and receive
  cycle-bounded deterministic facts, including period metadata and merchant /
  member attribution. Insights now compare against the actual preceding
  confirmed salary anchor, and Telegram supports a bounded cycle-insight query
  whose validated facts are sent through the cloud LLM gateway for narrative
  analysis, with deterministic fallback if the gateway is unavailable.
- [x] Cycle-aware foundation: authenticated `GET /api/v1/analytics/cycle`
  resolves the current confirmed primary-salary cycle in Asia/Jakarta and
  explicitly falls back to the calendar month when salary setup is absent.
- [x] Ringkasan KPIs now use the active confirmed salary cycle when configured;
  Analisis provides explicit Siklus Gaji/Kalender period controls.
- [~] Indonesian copy and chart/UI polish. Primary navigation, settings,
  document intake/detail, analytics headings, and cycle labels now use
  consistent Indonesian title case (including `Siklus Gaji`). Charts now have
  clearer empty states, accessible labels, stronger tooltip hierarchy, and
  improved visual contrast. Remaining refinement is still scheduled.
- [~] Invoice flow. Invoice/tagihan documents now enter the transaction-proof
  extractor with an explicit `payment_status` field. A row is eligible only
  when the document says `PAID`; matching links evidence to an existing expense
  and unmatched rows use the existing review flow. Unpaid/unclear invoices
  remain `NEEDS_REVIEW`; due-date handling and richer invoice UX remain
  scheduled.
- [ ] Known-account hardening and off-host backup.

## 1–4 delivery evidence

- [x] 1. Telegram callback fast path and dedicated interactive execution lane.
- [x] 2. Multi-image document intake and ordered page preservation.
- [x] 3. Universal review routing and household-wide delivery.
- [x] 4. Payslip-driven salary source/event and dynamic current-cycle views.
  Missing-pay-date review completion, exact prior-cycle comparisons, and the
  dedicated Telegram cycle-insight query and LLM-backed narrative are shipped.
