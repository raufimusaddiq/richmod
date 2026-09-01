# Conversational Native Tool V2 — Completion Checklist

Source: `docs/RICHMOD_CONVERSATIONAL_NATIVE_TOOL_V2.md`.

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
- [ ] Full review-type action descriptor mapping.
- [ ] DB-backed conversation/review/lane integration coverage.
- [ ] Production rollout/deploy smoke.

Latest implementation tracking: `docs/CONVERSATIONAL_NATIVE_TOOL_V2_COMPLETION_CHECKLIST_2026-09-01.md`.
