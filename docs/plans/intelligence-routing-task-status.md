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
pass (Go runs capped at 1 CPU / 1 GiB). The added
`TestConfirmRefusesPartialCompoundResidualUntilAllSupplied` exercises the API
handler against PostgreSQL: category-only supply returns 409 naming only
`transaction_at` and leaves the transaction in review; supplying category and
date confirms the same canonical transaction and resolves the review.
Drift checklist: A pass; B pass (same existing calls/order); C pass; D pass; E
pass; F pass; G pass; H Telegram/receipt/screenshot/bank/payslip behavior not
otherwise changed; I unchanged; J pass; K pass.
Known follow-up: none for the current API and Telegram confirmation surfaces;
re-audit any newly added canonical confirmer against the stored residual
contract.

IR-02 follow-up: the conversational Telegram agent confirmation path now uses
the same stored residual guard; blocked confirms return the exact missing facts
instead of a generic turn failure. A supplied date is strict `YYYY-MM-DD` and
is persisted to the transaction and proposal. If merchant learning attempts an
auto-confirm while another residual remains, the saved detail is committed and
the review stays open. Database-backed Telegram package tests pass against
disposable PostgreSQL 17 (1 CPU / 1 GiB). API compound-residual integration
test and all migrations through 65 pass against disposable PostgreSQL 17; the
test asserts zero model calls by exercising the deterministic confirmation API
directly. Current `main` CI also passes the complete API/worker suites and vet.

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

## IR-10 — Regression, canary, and rollout hardening

Task: IR-10
Baseline main SHA: `d2003de1189a368ca3f9a8c56d3de975ad32941c` (merged PR #150)
Files changed: `apps/worker/internal/telegram/{processor.go,transaction_decision.go}`,
`apps/worker/cmd/worker/main.go`, `apps/worker/internal/telegram/agent_single_pass_test.go`,
`.env.example`, `docs/runbooks/production-deployment.md`.
User interactions before: a complete Telegram generative extraction confirmed
directly with no independent rollback scope; disabling any source switch left the
conversational path unchanged.
User interactions after: unchanged by default (switch on). When
`RICHMOD_AUTOCONFIRM_TELEGRAM=0`, a complete extraction parks in review with the
same extracted facts proposed, so the residual-date/category contract already in
IR-01/IR-02 governs; no extra Jev or user step is introduced.
Jev calls before/after: unchanged. Direct acceptance still costs zero Jev; the
disabled switch also costs zero Jev, so rollback cannot silently re-open a full
semantic replay.
Generative calls before/after: unchanged; the switch gates Go's post-extraction
acceptance, not the extraction itself.
Canonical correctness guard: Go still owns the CONFIRMED transition; the
disabled switch only removes auto-confirm, never the validation, so
category/date/amount/duplicate guards keep running. Payment facts remain in the
stored proposal for minimal review.
Residual uncertainty after: on rollback, the whole proposal is reviewable rather
than a named residual, which is the intended fail-safe behaviour of a rollback
switch, not a new residual contract for the happy path.
Tests added/updated: `TestPostGenerativeAutoConfirmSwitchParksReview` asserts the
disabled switch neither authorizes confirmation nor spends a Jev call; existing
IR-04 direct-acceptance, residual-category, missing-date, ambiguity, and route
authority tests remain the regression matrix. The opt-in
`TestRealLiteRouterSystemOneSmoke` is the synthetic receipt residual-category
semantic canary: one real `/v1/systemone` call must select the evidence-supported
category, with no LLM comparison or canonical write.
Migration/schema changes: none.
Drift checklist: A pass (no new user input; rollback is opt-in); B pass (no new
model call; switch only removes an auto-confirm); C pass (semantic owner
unchanged); D pass (validation/thresholds untouched); E pass (no fact invented;
facts preserved for review); F pass (IR-01/02 residual contract still gates
review); G pass (Jev failure semantics unchanged); H Telegram pass (simple path
still Jev-only, complex path still no full replay); I pass (telemetry untouched);
J pass (switch behaviour asserted); K pass (runbook table and env example
updated; `git diff --check`).
Known follow-up: canary smoke requires an approved LiteRouter credential/network
environment and is not run in ordinary CI; deployment remains subject to the
sprint release/approval flow.

IR-10 rollout-scope audit: PRD §17 requires receipt/screenshot switches to
independently reverse direct auto-confirm and residual category rescue. The
follow-up implementation gates those calls too; rollback preserves category-only
reviews and performs zero Jev calls. Eight source regression tests, including
both new switch-off cases, pass against disposable PostgreSQL 17; the full worker
document package passes.

IR-10 follow-up completion record:
Task: IR-10 rollout-switch scope
Baseline main SHA: `fb23a97c153f2a18ab5da04eef3179225d0de2c9`
Files changed: `apps/worker/internal/document/{receipt.go,screenshot.go}`;
receipt and screenshot integration tests; this ledger; production deployment
runbook switch table.
User interactions before: with receipt/screenshot switches disabled, direct
confirmation stopped but residual category Jev still ran and could confirm,
leaving only the eventual human work disabled.
User interactions after: switches on = unchanged minimal flow; switches off =
one category-only review when category is residual, with no new fields or guesses.
Jev calls before: switch off still allowed one receipt rescue or one selective
screenshot batch; switch on was 0 for clear, 1 for residual receipt/one batch for
residual screenshot rows.
Jev calls after: switch off = 0 for either source; switch on unchanged (clear 0,
receipt residual 1, screenshot residual rows batched in 1 call).
Generative calls before/after: unchanged; one vision extraction, and existing
conditional repair only.
Canonical correctness guard: with the switches off, neither source auto-confirms
from the disabled rescue; extracted evidence persists in review with
`missing_facts=["category"]`. Duplicate/date paths unchanged.
Residual uncertainty after: category only; transaction date is never guessed.
Tests added/updated: receipt/screenshot switch-off integration tests assert zero
Jev plus `NEEDS_REVIEW` and category-only ReviewDecision; 8 focused database tests
pass; full `go test ./internal/document/` passes against disposable PostgreSQL 17.
Drift checklist: A pass; B pass (switch removes bounded rescue and auto-confirm
together); C pass; D pass (thresholds unchanged); E pass (facts not invented); F
pass (exact category residual retained); G pass (no model failure used as
approval); H receipt/screenshot pass; I pass (existing phase telemetry records
only executed calls); J pass (canonical and call counts pinned); K pass (runbook
and task ledger updated; `git diff --check`).
Known follow-up: merged in PR #154 (merge commit
`c91e30dc9119f4c04ff98e8d1f19b98f121fd7d0`); main CI, CodeQL, and Release
Images published immutable `sha-c91e30d` images. Deployment not requested.

## IR-09 — Intelligence-pass telemetry

Task: IR-09
Baseline main SHA: `d2003de1189a368ca3f9a8c56d3de975ad32941c` (merged PR #150)
Files changed: `db/migrations/00065_intelligence_phase_telemetry.sql`;
`apps/worker/internal/judgment/{judgment.go,systemone/{client.go,decorate.go}}`;
`apps/worker/internal/gateway/{client.go,agent.go}`;
`apps/worker/cmd/worker/main.go`; document/bank-email/financial-email/telegram
call sites; `apps/api/internal/operations/judgment.go` and its integration test;
`docs/DATABASE_SCHEMA.md`.
User interactions before/after: unchanged. Telemetry is additive; no review is
added, removed, or reordered by this task.
Jev calls before/after: unchanged. Each bounded call now writes one
`intelligence_phase_telemetry` row (`capability='JEV'`) through the reader-side
`InstrumentedEngine`, which wraps the engine so test and production engines
report identically. `llm_call` still records the transport call once.
Generative calls before/after: unchanged. Generative phases report capability,
purpose (from task/phase metadata), the semantic field names in the tool
arguments, and the returned field names; they carry no prompt, answer, or value.
Canonical correctness guard: telemetry is read-only with respect to canonical
state; Go still owns every state transition. Phase rows store only field names,
policy/model, latency, and transport outcome. Source-event correlation prefers
the trusted caller context over a request ID, so a document ID can never be
mistaken for a source event; the migration's `household_id` foreign key stays
authoritative.
Residual uncertainty after: residual rescue success is only reported when a
Go-written `judgment_decision` row with a matching category outcome exists.
Transport success is never reported as policy acceptance; the category
double-pass figure is explicitly a candidate, because generative output alone
cannot prove Go accepted the category.
Tests added/updated: `InstrumentedEngine` exactly-once + source-event preference
(`decorate_test.go`); API phase-order aggregate including valid vs redundant
category pass, residual rescue attempt/success, and p50 latency
(`judgment_integration_test.go`). Existing IR-04..IR-08 canonical tests remain
green on disposable PostgreSQL 17 with Go capped at 1 CPU / 1.5 GiB.
Migration/schema changes: `00065_intelligence_phase_telemetry.sql` (new table,
two indexes); `docs/DATABASE_SCHEMA.md` ERD, table entry, and schema version.
Drift checklist: A pass (no RHICE change; no new user input); B pass (no new
model call; instrumentation wraps existing calls); C pass (no semantic owner
change; telemetry never mutates canonical state); D pass (no threshold or
validation changed); E pass (no fact invented; only field names recorded);
F pass (review contracts untouched); G pass (transport failure stays distinct
from semantic outcome; no retry behaviour changed); H pass (call ordering per
source unchanged); I pass (migration 65 is telemetry-only, forward and down);
J pass (no canonical write added; `go test`/`go vet` on affected API and worker
packages against disposable PostgreSQL); K pass (schema reference updated;
`git diff --check` clean).
Known follow-up: IR-10 regression/canary/rollout hardening; no IR-09 production
deployment until the sprint release/approval flow.

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
