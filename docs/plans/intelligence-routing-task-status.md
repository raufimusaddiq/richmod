# Intelligence Routing Task Status

This ledger follows `docs/plans/intelligence-routing-minimal-interaction-execution.md`.
Do not mark a task complete until its full drift guard and completion template
are recorded.

## IR-00 — Baseline re-audit

Task: IR-00
Baseline main SHA: `b79bd7b1248884d55bb5dd1c23a720b68dd9b056`
Files changed: `docs/plans/intelligence-routing-baseline-audit.md`
User interactions before: existing residuals can be mislabeled; review UI may
not render the exact missing fact.
User interactions after: IR-01/02 work represents exact residual dimensions;
other source paths unchanged.
Jev calls before: unchanged.
Jev calls after: unchanged.
Generative calls before: unchanged.
Generative calls after: unchanged.
Canonical correctness guard: audited receipt/screenshot fallback dates, review
contracts, API and Telegram confirm paths; baseline remains ancestor.
Residual uncertainty after: task-specific uncertainties remain for IR-01..IR-10.
Tests added/updated: none (audit-only).
Drift checklist: A–K reviewed; no implementation change existed at IR-00 start;
all applicable invariants preserved.
Known follow-up: none for IR-00.

## IR-01 — Residual ReviewDecision correctness

Task: IR-01
Baseline main SHA: `b79bd7b1248884d55bb5dd1c23a720b68dd9b056`
Files changed: receipt/screenshot decision builders and tests; shared reason
presets.
User interactions before: receipt/screenshot fallback date could be shown as
known; review reasons could omit date residual or compound missing facts.
User interactions after: cards name category-only, date-only, both facts, or
duplicate ambiguity; no source-derived fallback date is presented as observed.
Jev calls before: receipt 0 Jev; screenshot one batched Jev call for unmatched
expense rows requiring category choice.
Jev calls after: unchanged; residual contract logic is deterministic.
Generative calls before: receipt 1 vision/extraction call; screenshot 1 vision/
extraction call.
Generative calls after: unchanged.
Canonical correctness guard: duplicate ambiguity takes precedence; fallback
timestamps remain internal canonical placeholders plus provenance, never known
source facts.
Residual uncertainty after: exactly `category`, `transaction_at`, both, or
`duplicate_relationship` as appropriate.
Tests added/updated: receipt and screenshot unit checks; worker document/reviewdec
packages pass.
Drift checklist: A pass; B pass (no model ordering/call-count change); C pass;
D pass (no extraction acceptance change); E pass; F pass; G pass (failure
semantics unchanged); H receipt/screenshot pass for review facts, clear row
routing remains IR-05/06; I unchanged; J pass; K pass.
Known follow-up: revalidate across IR-05/06 residual rescue and batching tasks.

## IR-02 — Canonical confirmation guard

Task: IR-02
Baseline main SHA: `b79bd7b1248884d55bb5dd1c23a720b68dd9b056`
Files changed: API and Telegram review confirmation guards; web residual-date
field and unit/integration seams.
User interactions before: a legacy or stale confirm request could confirm while
stored `missing_facts` still named a required date/category.
User interactions after: API rejects unresolved required residuals with 409;
Telegram leaves review open and asks for the exact residual; web exposes a native
date input only when `transaction_at` remains missing.
Jev calls before: 0 for explicit human review confirmation; Telegram may use its
existing generative classification call for a free-form category response.
Jev calls after: unchanged.
Generative calls before: unchanged from existing review-reply flow.
Generative calls after: unchanged.
Canonical correctness guard: category validated household-scoped; date parsed as
strict `YYYY-MM-DD` in Asia/Jakarta, persisted to transaction and proposal;
fallback timestamp does not satisfy residual date.
Residual uncertainty after: remains in review until all canonical-required
stored residual facts are supplied.
Tests added/updated: API blocker tests, Telegram residual/date tests, web source
test; API review, Telegram, document/reviewdec packages and 59 frontend tests
pass (Go runs capped at 1 CPU / 1 GiB).
Drift checklist: A pass; B pass (same existing calls/order); C pass; D pass; E
pass; F pass; G pass; H Telegram/receipt/screenshot/bank/payslip behavior not
otherwise changed; I unchanged; J pass; K pass.
Known follow-up: run database-backed canonical/legacy confirm integration tests
with `TEST_DATABASE_URL`; extend guard to other canonical confirm actions only
if their decision contract identifies required residuals.
