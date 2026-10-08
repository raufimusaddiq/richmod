# Conversational Native Tool V2 — Completion Checklist

Source: `docs/archive/RICHMOD_CONVERSATIONAL_NATIVE_TOOL_V2.md`.

- [x] Native-only worker finance runtime; static guard added.
- [x] Required single native tool call enforcement.
- [x] Strict generic native tool argument decoder.
- [x] Telegram one-decision native path; no structured/regex fallback.
- [x] Required native document extraction: classification, payslip, receipt, screenshot.
- [x] Telegram turn persistence and bounded recent context.
- [x] Context-aware native tool catalog; no model-facing pending UUID.
- [x] CHAT lane migration, routing, index, and configurable worker concurrency.
- [x] Native call telemetry fields: `call_kind`, `tool_name`.
- [x] Legacy API structured LLM client removed.
- [x] ADR-030, ADR-031, ADR-032 added.
- [x] Scoped ephemeral transaction-reference mapping for multi-result correction.
- [x] Salary-choice and merchant-learning native tools.
- [x] Review action descriptors are bounded by review type; required pay-date/bank fields are validated before mutation.
- [x] Existing Telegram/document/bank DB integration suites pass against disposable PostgreSQL through migration 40.
- [x] Production rollout/deploy smoke. Production CHAT lane has 19 succeeded `PROCESS_TELEGRAM_TEXT` jobs and 0 failed (checked 2026-10-03).

Latest implementation tracking: `docs/archive/CONVERSATIONAL_NATIVE_TOOL_V2_COMPLETION_CHECKLIST_2026-09-01.md`.
