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

IR-02 follow-up: the conversational Telegram agent confirmation path now uses
the same stored residual guard; blocked confirms return the exact missing facts
instead of a generic turn failure. A supplied date is strict `YYYY-MM-DD` and
is persisted to the transaction and proposal. If merchant learning attempts an
auto-confirm while another residual remains, the saved detail is committed and
the review stays open. Database-backed Telegram package tests pass against
disposable PostgreSQL 17 (1 CPU / 1 GiB).

## IR-03 — RHICE measurement correctness

Task: IR-03 (complete)
Baseline main SHA: `b79bd7b1248884d55bb5dd1c23a720b68dd9b056`
Files changed: `apps/api/internal/operations/product.go`,
`apps/api/internal/review/canonical.go`,
`db/migrations/00061_product_telemetry_events.sql`, related tests.
User interactions before: one typed-field action could count as one regardless
of supplied fields; financial-email server-merged IDs could look user-entered;
telemetry without a canonical review cohort could enter RHICE.
User interactions after: each supplied field or bounded choice counts once;
typed fields use bounded normalized names, with financial-email fields marked
before server merge; only canonical-cohort review turns count. `MERGE_REVIEW` remains a human choice;
`IGNORE` and system resolutions do not count.
Jev calls before/after: unchanged.
Generative calls before/after: unchanged.
Canonical correctness guard: telemetry only; no financial state transition or
inference behavior changed.
Residual uncertainty after: unchanged.
Tests added/updated: aggregate fixtures assert amount + date as two typed inputs,
acceptance as one bounded choice, empty confirmation/IGNORE/non-cohort turns as
zero, and canonical-cohort RHICE; financial-email resolution asserts only the
user-supplied entity is credited. Ran migration 61 + `go test` for
`apps/api/internal/operations/...` and `apps/api/internal/review/...` against a
disposable PostgreSQL 17 (1 CPU / 1 GiB) — pass. Migration 62 and the new
review-type/provenance tests also pass; full worker tests and affected API tests
pass locally. PR CI passed API/worker tests and vet, frontend, container build,
CodeQL, secrets, and Hermes review at `ec0f3af`.
Drift checklist: A pass; B pass; C pass; D pass; E pass; F pass; G pass; H
unchanged; I pass (user-supplied fields/bounded choices counted, server-merged
values excluded); J pass for the bounded DB set; K pass (`git diff --check`).
Known follow-up: IR-09 must consume these measured turns for call-efficiency
analysis; no IR-03-specific correctness or telemetry gap remains.

## IR-04 — Telegram single-pass routing

Task: IR-04
Baseline main SHA: `e411dc56ccee68e0a405145d63fb542c14faaa43`
Files changed: Telegram conversational transaction routing, transaction tool
schema, turn telemetry, migration 63, schema reference, and focused tests.
User interactions before: complete extracted transactions could receive a
redundant Jev replay and then a generative synthesis; missing date remained a
review blocker.
User interactions after: complete valid transactions commit with deterministic
result text; missing date or explicit ambiguity stays in Review; similar
transactions keep the explicit edit-confirmation prompt.
Jev calls before: simple harvestable = 1; complex clear = route + full semantic
replay (2); category residual = route + full semantic replay (2).
Jev calls after: simple harvestable = 1; complex clear = route only (1);
category residual = route + category-only rescue (2).
Generative calls before: simple harvestable = 0; complex clear/residual = 1.
Generative calls after: unchanged; route failure exposes no mutation authority.
Canonical correctness guard: deterministic acceptance rechecks type, positive
integer IDR amount, user-supported date provenance, allowed active household
category, ambiguity flag, server-owned mutation route, household identity, and
similar-transaction conflict. Missing dates and unscoped ambiguity cannot
direct-confirm.
Residual uncertainty after: `transaction_at` remains user-resolved; category
alone may receive one Jev rescue; ambiguous extraction remains in Review.
Tests added/updated: direct acceptance/no replay; category-only rescue; missing
date and explicit ambiguity fail closed; mutation-route authorization; complex
and simple call budgets; Jev route outage; existing edit prompt; open-review
precedence. Full Telegram package and `go vet` pass against disposable
PostgreSQL 17; migration 63 up/down/up passes in capped containers (1 CPU / 1 GiB
Go, 0.5 CPU / 512 MiB PostgreSQL).
Drift checklist: A pass (no unnecessary user prompt; correction guard retained);
B pass (complete extraction skips full replay; only category residual rescued);
C pass (Go owns validation and IDs; generation and Jev own distinct facts);
D pass (confidence is not an acceptance gate; route, schema, amount, date,
category, household, ambiguity, and duplicate guards retained); E pass (missing
date is not guessed); F unchanged (review facts remain canonical); G pass (route
failure and residual uncertainty fail closed); H Telegram pass (simple, clear,
residual, binding, open-review cases); I pass (phase lane and residual dimension
recorded without raw facts); J pass (canonical state and model call budgets
asserted); K pass (`git diff --check`, migration scope only).
Known follow-up: IR-09 consumes the new residual lane for call-efficiency
analysis. The legacy deterministic `Process` path remains for callbacks; regular
text jobs enter through `ProcessAgent`.

## IR-05 — Receipt residual bounded rescue

Task: IR-05
Baseline main SHA: `d4ea586edfbd35a567f74d66cf1411a071bc1b7d`
Files changed: `apps/worker/internal/document/receipt.go`,
`apps/worker/internal/document/receipt_choice.go`, receipt integration tests.
User interactions before: a receipt whose only unresolved fact was the category
always opened a category review, even when a bounded ruling could safely decide
it.
User interactions after: a decisive category rescue confirms directly; an
undecided, provider-failed, dateless, or duplicate-ambiguous receipt keeps its
minimal review with only the residual fact asked.
Jev calls before: clear category = 0; category residual = 0 (review instead).
Jev calls after: clear category = 0; category residual = exactly one category
rescue, and only when the category is the sole bounded residual.
Generative calls before/after: unchanged; vision extraction stays the only
generative call.
Canonical correctness guard: rescue runs only with a printed date, no duplicate
candidate, and at least two allowed categories, and commits only on a decisive
well-separated answer. Missing date is never a rescue input and upload time is
never the transaction date. Provider failure is recorded as `PROVIDER_FAILURE`,
never approval.
Residual uncertainty after: undecided category remains in review; absent date
remains user-resolved; duplicate ambiguity remains duplicate review.
Tests added/updated: R-clear unchanged; R-category-rescue confirms with one Jev
call and `category_rescued` provenance; R-category-undecided keeps a category-only
review; R-date-missing sends no Jev call and asks only the date; provider failure
keeps review; R-duplicate unchanged. Full document package and `go vet` pass
against disposable PostgreSQL 17 with capped resources.
Drift checklist: A pass (review removed only when it was avoidable); B pass (one
bounded rescue, no replay, no per-row calls); C pass (Go owns validation, IDs,
and the canonical write); D pass (no confidence-only gate; date, duplicate,
category, and authorization guards retained); E pass (missing date never
guessed); F pass (review facts remain canonical and now persisted as bounded
provenance); G pass (provider failure fails closed); H receipt pass; I pass
(bounded call recorded with policy version and outcome); J pass (canonical state
and Jev call counts asserted); K pass (no schema change, `git diff --check`).
Known follow-up: IR-09 consumes the new bounded-call provenance; IR-06 addresses
the screenshot row batch.

## IR-06 — Screenshot selective bounded batch

Task: IR-06
Baseline main SHA: `87b01e05e29474a47d8dc278b37c68c09bbf87db`
Files changed: screenshot validation, selective category rescue, and focused
unit/database integration tests.
User interactions before: accepted vision categories were incorrectly left
unresolved, so they entered the Jev batch with genuinely uncertain rows.
User interactions after: only unmatched expense rows without a policy-accepted
vision category enter the single batch; decisive residuals confirm, unresolved
rows review. Clear rows retain extraction-only provenance.
Jev calls before/after: one per screenshot when at least one unresolved expense
category exists; zero when all rows are clear. Jev questions are residual row
count only.
Generative calls before/after: one vision extraction; unchanged.
Canonical correctness guard: household category allowlist, source confidence
threshold, duplicate/reconciliation matching, date and extraction confidence,
category conflict and auto-confirm kill switch remain enforced.
Residual uncertainty after: only missing/uncertain category, date, transfer
classification, or duplicate relationship.
Tests added/updated: 20 rows with 17 clear and 3 residual assert 3 questions,
2 decisive rows and 1 review; database integration asserts 19 canonical rows,
one residual review, summary counts and question provenance excludes clear rows.
Migration/schema changes: none.
Drift checklist: A pass; B pass (clear rows skip Jev); C pass (Go maps slugs to
household IDs); D pass (no thresholds relaxed); E pass (no missing date guessed);
F pass; G pass (undecided/provider failure does not confirm); H screenshot pass;
I pass (only questioned rows appear in provenance); J pass; K pass (`git diff
--check`). Full document package and `go vet ./internal/document` pass against
disposable PostgreSQL 17, with Go capped at 1 CPU/1 GiB and PostgreSQL at 0.5
CPU/512 MiB.
Known follow-up: IR-09 consumes bounded-call provenance.

## IR-08 — Payslip residual policy separation

Task: IR-08
Baseline main SHA: `6d47f63fe51a4484e0353084daca13dd3742b66e`
Files changed: payslip ReviewDecision construction, canonical review listing and
resolution, review UI, RHICE date-field normalization migration 64, schema
reference, and focused tests.
User interactions before: the missing-date card always asked for salary
classification, including when a primary salary already existed; the server
also required a policy choice on every missing-date resolution.
User interactions after: established primary salary + missing date asks for the
date only and preserves the existing primary policy; first salary + clear date
asks only `PRIMARY_SALARY` vs `ORDINARY_INCOME`; first salary + missing date
shows both date and policy inputs. RHICE counts the supplied date plus the
policy choice as two inputs in the combined case.
Jev calls before: 0 on clear payslip extraction; 0 on missing-date and salary
policy review.
Jev calls after: unchanged, 0 in all three cases; salary designation remains
human-owned, not model-inferred.
Generative calls before: one vision extraction on a valid payslip; the existing
single constrained repair call remains conditional on extraction validation
issues.
Generative calls after: unchanged; no model replay added. Unit assertion pins
one payslip extraction call and no Jev verifier route.
Canonical correctness guard: live household salary state is read while listing
and again under the resolution transaction; the API list reconciles missing
policy dimensions/actions for reviews whose household policy changed while open.
The locked ReviewDecision action set gates actions and salary choices; current
household state also rejects redundant reclassification. Date remains strict
`YYYY-MM-DD`; Go alone confirms transaction/evidence/salary state. Date-only
resolution adds/updates a non-primary salary source, never changes the existing
primary. No model decides salary designation.
Residual uncertainty after: only `transaction_at` when primary salary policy
already exists; only `salary_classification` when date is clear and no primary
exists; both when both are unresolved.
Tests added/updated: three ReviewDecision shapes; API listing assertion for E1;
database-backed E1 confirms date and existing primary unchanged, and E2 rejects
date-only resolution, accepts explicit policy, confirms canonical salary event,
and asserts RHICE counts 1 typed date + 1 bounded choice. Payslip model-call
count/no-Jev check; Web conditional-selector test. API review and worker
document tests plus vet pass on disposable PostgreSQL 17 with Go capped at 1 CPU
/ 1 GiB; Web focused test passes.
Drift checklist: A pass (removes known redundant selector; E2 retains exactly
two genuine inputs); B pass (generative vision remains required, no Jev replay);
C pass (vision owns extraction, user owns policy, Go owns canonical state); D
pass (existing schema/date/arithmetic/household validation unchanged); E pass
(missing date remains missing until user supplies it); F pass (exact residuals,
stored action validation, API resolution guard); G pass (no inference fallback
or unsafe model retry); H Payslip pass (E1 date-only; no existing primary means
policy remains explicit); I pass (migration maps `payDate` to RHICE
`transaction_at`, combined choice counted once); J pass (canonical state,
RHICE count, one vision/zero Jev asserted); K pass (migration 64 is telemetry
only; no unrelated source/UI scope; schema reference updated; `git diff --check`).
Known follow-up: IR-09 consumes phase telemetry; no IR-08 production deployment
until the sprint release/approval flow.

## IR-07 — Bank Email merchant-less category resolution

Task: IR-07
Baseline main SHA: `ce756647c7c073d5f79f4ea383f099ff4f519827`
Files changed: bank-email category routing/resolver and focused tests.
User interactions before: a valid merchant-less expense entered `UNKNOWN_MERCHANT`
and skipped the bounded category decision.
User interactions after: when description or counterparty evidence exists, the
existing Jev category decision may resolve the expense with merchant still NULL;
provider failure, ambiguity, or absent category evidence remains reviewable.
Jev calls before/after: zero for merchant-less expenses; one category attempt
when useful supporting text exists.
Generative calls before/after: unchanged; generic extraction remains the only
generative call.
Canonical correctness guard: evidence text is marked untrusted; categories and
IDs remain household-scoped; Go alone writes canonical state; merchant is never
fabricated; the category auto-confirm kill switch still gates the path.
Residual uncertainty after: undecided merchant-less category remains the
existing `UNKNOWN_MERCHANT` review; no description/counterparty means no Jev call.
Tests added/updated: decisive merchant-less category, NULL merchant, provider
failure, and amount-only no-call coverage.
Migration/schema changes: none.
Drift checklist: A pass (review removed only when bounded evidence decided it);
B pass (one category attempt, no replay); C pass (Go resolves IDs and writes);
D pass (no threshold relaxed, kill switch preserved); E pass (no merchant
fabricated); F pass; G pass (provider failure and absent evidence keep review);
H pass; I pass (bounded call carries policy version); J pass (canonical IDs and
NULL merchant asserted); K pass (`git diff --check`).
Known follow-up: IR-09 consumes bounded-call provenance.
