# Document and Insight Native Tools with Failure Acknowledgement

## Scope

- [x] Use required `classify_financial_document` native tool for Telegram/web
  document classification.
- [x] Keep Go enum, confidence, unknown-field, and trailing-JSON validation.
- [x] Keep receipt, payslip, and screenshot extraction architecture unchanged.
- [x] Keep one native tool call maximum for classification.

## Terminal failure behavior

- [x] After terminal `PROCESS_DOCUMENT` failure, mark document and source event
  `NEEDS_REVIEW`.
- [x] Create one canonical `DOCUMENT_CLASSIFICATION` review item.
- [x] Enqueue one Telegram failure acknowledgement for Telegram-originated
  documents, deduplicated for 24 hours.
- [x] Do not expose gateway/provider error details to Telegram users.
- [x] Preserve web-originated failures without sending Telegram messages.

## Insight generation

- [x] Use required `write_financial_insight` native tool instead of structured
  text output.
- [x] Keep strict Go validation for narrative length, observations, confidence,
  unknown fields, and trailing JSON.
- [x] Allow immediate regeneration after a failed insight; pending and recent
  successful insights remain idempotent.
- [x] Keep deterministic low-completeness fallback unchanged.

## Verification

- [x] Live sanitized 9router `responses` stream returned one native
  `classify_financial_document` call using `finance-tracker`.
- [x] Live sanitized 9router `responses` stream returned one native
  `write_financial_insight` call using the exact Richmod prompt contract.
- [x] Worker document and gateway tests pass.
- [x] Worker vet passes.
- [x] Disposable PostgreSQL integration test verifies review, state updates,
  acknowledgement job, and idempotency.
- [ ] Commit, push, merge, deploy, and reclaim after approval.
