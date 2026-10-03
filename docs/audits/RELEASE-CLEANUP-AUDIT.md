# Release cleanup audit: dead code, dead features, comments and tests

**Baseline:** `10d1793` (`main`, 2026-10-03)
**Status:** Executed on branch `chore/release-dead-code-audit` (see section 13).
Sections 3-10 are the audit as written before any change.

Goal for the release tag: the tree contains only code that production runs or
that tests a behaviour production runs, comments that explain *why* without
citing sprint history, and tests that exercise behaviour rather than freeze
source text.

## 1. Method

| Area | Tool | Notes |
| --- | --- | --- |
| Go functions | `golang.org/x/tools/cmd/deadcode` over the whole workspace (`apps/api`, `apps/worker`, `apps/reviewdomain`), with and without `-test` | Whole-program reachability from `cmd/api`, `cmd/bootstrap`, `cmd/worker`. Run per module first; `reviewdomain` alone has no `main`, so its 48 hits were false positives and are not listed. |
| Go unexported symbols | `staticcheck` (all checks minus style-only `ST1000/1003/1005/1016/1020-1022`, `SA1019`) | Catches unused unexported funcs, dead stores, broken loops. |
| Go exported types/consts/vars | Custom `go/packages` pass: every exported package-level object with zero non-test uses across the workspace | `deadcode` does not see types or constants. |
| Web files and exports | Import graph from Next entry files (`page/layout/route/opengraph-image`) | Distinguishes "no importer" from "imported only by tests". |
| API surface | Every `mux.Handle` route vs. every `fetch` path in `apps/web/app` (including dynamic `${...}` segments) | |
| Job queue | Every job `type` the worker lanes vs. every Go producer | |
| CSS | Every class selector vs. every literal and template class in JS | Template prefixes (`status-${…}`, `admin-${…}`) treated as used. |
| Config | Every variable in `.env.example` / Compose vs. code readers | |
| Comments | grep for sprint tags (`CEU-`, `SAVR-`, `UIR-`, `UIRC-`, `IR-`, `UISC-`), `PRD §`, `Hermes` (review-bot rounds), commented-out code | ADR/BDR references are durable and kept. |
| Tests | grep for tests that `ReadFile` source/CSS and regex-match it, sprint-named test files, `t.Skip` | |

`go vet` is clean in all three modules. No commented-out code was found.
Environment variables are clean (the only unread ones feed the Postgres and
backup containers, not the apps).

## 2. Summary

| # | Category | Findings | Needs decision |
| --- | --- | --- | --- |
| A | Dead Go code (never reached, not even by tests) | 7 functions, 1 dead store, 1 fake retry loop | no |
| B | Go code reached only by tests | 13 symbols | 5 kept as test seams, 8 removable |
| C | Orphan job types | 2 | no |
| D | Web: dead exports, test-only logic, dead CSS | 16 exports, 1 dead UI feature, 11 CSS selectors | no |
| E | Dead features (whole capabilities nobody can reach) | 3 | **DECIDE** |
| F | AI-slop comments | ~260 tagged references in 55 production files | style rule below |
| G | Source-text "lock" tests | ~250 regex-on-source assertions in 8 web test files, 10 Go files | **DECIDE** scope |
| H | Historical docs | ~60 top-level PRDs/checklists/briefs | **DECIDE** |

## 3. A — Dead Go code (unreachable from `main` and from every test)

All of these are safe deletes: no production or test caller exists.

| Location | Symbol | Why it is dead | Action |
| --- | --- | --- | --- |
| `apps/api/internal/analytics/detail.go:219` | `(*Handler).currentPeriod` | Superseded by `analyticsRange`, which inlines the same calculation. | Delete |
| `apps/api/internal/document/handler.go:251` | `(*Handler).persist` | Old upload persistence; upload now goes through the storage-backed path. | Delete |
| `apps/api/internal/emailingress/service.go:363` | `(*Service).mustCurrent` | No caller. | Delete |
| `apps/api/internal/review/finalize.go:13` | `resolveTransactionReviewItem` | Thin wrapper over `reviewdomain.ResolveByTransaction`; callers use the domain function directly. The file contains nothing else. | Delete file |
| `apps/worker/internal/document/evidence.go:135` | `EvidenceContext.modelContent` | Already listed as dead in the P0 audit; still dead. | Delete |
| `apps/worker/internal/financialemail/classification.go:253` | `ObservationClassification.nonActionable` | No caller. | Delete |
| `apps/worker/internal/document/repair.go:150` | `fieldsRaw = …` | Dead store (SA4006): the value is written into `arguments` and re-read from there. | Simplify |
| `apps/api/internal/emailingress/service.go:83` | `for attempt := 0; attempt < 3; …` | Fake retry loop (SA4004/SA4008): every path returns on the first pass. A random 128-bit local part cannot collide, and the only real conflict (one address per household) already returns the existing row. | Replace with straight-line code; behaviour unchanged |

## 4. B — Go code reached only by tests

Production never calls these; only tests keep them alive. Two kinds:

**Keep (legitimate test seams, cross-package so they cannot live in `_test.go`):**

| Symbol | Used by | Reason to keep |
| --- | --- | --- |
| `auth.ContextWithPrincipal` | 65 call sites in 21 API test files | The way handler tests inject an authenticated principal. |
| `telegram.AgentFinanceTools` | 5 test files | Exported view of the live tool catalog for policy tests (kept on purpose by the P0 audit). |
| `reviewdomain.TelegramCallbackSamples` | worker `callback_grammar_test.go` | Lets the worker prove every callback it emits is admitted by the shared grammar. |
| `analytics.NewCycleHandler` | 4 analytics test files | Confirm during implementation; fold into `NewHandler` if it only differs by an injected clock. |
| `reviewdomain.FailedSourceInboxLink` | `failed_source_contract_test.go` | Confirm during implementation; delete if the test can assert the literal. |

**Remove (production code that exists only to be tested):**

| Symbol | Test that keeps it | Action |
| --- | --- | --- |
| `document.SalaryCycle` (`apps/worker/internal/document/salary_cycle.go`) | `salary_cycle_test.go`, `calculation_integration_test.go` | Salary cycles are computed by `financialperiod`; this is an unused second implementation. Delete the file and its test; drop the cross-check from the integration test. |
| `ObservationClassification.wealthAllowed` | `classification_test.go` (1 assertion) | Delete with its assertion. |
| `reviewdec.ActiveReasons` | `review_render_contract_test.go` | Delete if the test is in the same package (read `activeReasons` directly); otherwise keep. |
| `(*blob.Store).EvictLocal` | `store_test.go` | Delete with its test. |
| `auth.TenantFromPrincipal` | `service_test.go` | Delete with its test. |
| `document.NewHandler` (API) | 3 test files | Production uses `NewHandlerWithStorage`. Switch tests to it and delete the old constructor. |
| `(*telegram.Processor).HandleTerminalTextFailure` | `terminal_text_failure_integration_test.go` | Wrapper that opens a tx around the live `TerminalTextFailureTx`. Have the test open the tx and call the live function. |
| `(*telegram.Processor).bindRecentEvidence` | `evidence_recent_integration_test.go` (11 calls) | Wrapper over the live `bindRecentEvidenceFrom`. Have the test load candidates and call the live function. |

The unused exported constants in `reviewdec/decision.go`
(`HardCanonicalInvariant`, `RepresentationInvalid`, `HumanPolicy`,
`SourceJev`, `SourceDeterministicPlusJev`, `SourceUserPolicy`) are **kept**:
they belong to documented closed vocabularies, and `REPRESENTATION_INVALID` is
read by SQL in `operations/product.go`.

## 5. C — Orphan job types

| Job type | Producer | Worker handler | Where it still appears |
| --- | --- | --- | --- |
| `PROCESS_TELEGRAM_REVIEW_TEXT` | none | none | lane mapping in `apps/worker/internal/queue/queue.go:134`; DB lane trigger |
| `FINALIZE_TELEGRAM_MEDIA_GROUP` | none | none | lane mapping in `apps/worker/internal/queue/queue.go:136`; DB lane trigger |

Action: remove both names from the Go lane mapping. Leave the migrations alone
(historical migrations are immutable; the trigger branch is harmless).

## 6. D — Web

**Dead UI feature.** `insightQuality` and `completenessLabel`
(`apps/web/app/lib/insightData.js`) compute an insight "data quality" label.
No component renders it, and `cycle-review.test.mjs:119` asserts the card must
*not* show it. Only `insight-data.test.mjs` keeps the code alive. Delete both
functions and their test cases.

**Logic exported only for tests.** `deriveCycleSpendingMetrics` and
`cycleProgressLabel` (`app/lib/chartData.js`) have no production importer and
are tested in `chart-data.test.mjs`. Delete them and their test cases (or, if
the cycle chart is meant to use them, wire them in; current UI does not).

**Unneeded `export` keywords** (used only inside their own file; drop
`export`): `admin/shared.js: badgeLabel`, `analytics/CalendarReview.js:
ValueList`, `components/InboxCountProvider.js: INBOX_COUNT_EVENT`,
`components/review/shared.js: fieldLabels, label`, `lib/calendarReview.js:
MAX_CUSTOM_MONTHS`, `lib/cycleLedger.js: dayMonth`, `lib/format.js: rupiah`,
`lib/insightData.js: abortableDelay`. (`addMonths`, `monthSpan`,
`directionText` are used internally *and* by tests; keep exported.)

**Dead CSS selectors** (no element uses the class; about 35 rules):
`analytics-category-panel`, `analytics-detail-layout`, `analytics-detail-side`,
`analytics-kpis`, `analytics-ranked-card`, `invite-panel`, `kpi-grid`,
`login-brand`, `member-actions`, `member-list`, `ranked`. Remove the rules,
then check the visual-smoke baselines are unchanged.

## 7. E — Dead features — **DECIDE**

### E1. Monthly category budgets (ADR-014)

`GET/POST /api/v1/budgets`, `PATCH /api/v1/budgets/{id}` and
`apps/api/internal/budget/` exist, but **no web page calls them, and no worker,
Telegram tool or insight reads the `budget` table**. A household cannot see
or use budgets anywhere. Options:

1. **Remove** the handler, routes and their tests; keep the table and migration
   (no destructive schema change); mark ADR-014 superseded. *(Recommended for a
   "used features only" release.)*
2. Keep as an undocumented API-only feature.
3. Build the UI (out of scope for a cleanup).

### E2. HTTP routes with no in-repo caller

| Route | Handler |
| --- | --- |
| `POST /api/v1/transactions/{id}/confirm` | `ledgerHandler.ConfirmTransaction` |
| `POST /api/v1/transactions/{id}/void` | `ledgerHandler.VoidTransaction` |
| `POST /api/v1/reconciliation-merges/{id}/reverse` | `reviewHandler.Unmerge` |
| `GET/POST /api/v1/merchants`, `POST /api/v1/merchants/{id}/aliases` | `settingsHandler.Merchants`, `CreateMerchantAlias` |
| `GET /api/v1/analytics/spending` | `analyticsHandler.Spending` |
| `GET/POST /api/v1/admin/households/{householdId}/members` | `adminHandler.Members`, `AddMember` |

These are not dead *code* in the Go sense (the router reaches them), but no UI,
script or bot calls them. Void and un-merge are correction tools a household
may need, so the recommendation is to **keep void and reverse-merge** (and
consider exposing them in the UI later), and **remove** `analytics/spending`,
the `merchants` routes, and the admin household-member routes unless you use
them by hand (curl, ops scripts).

### E3. ADR-037 document-interpretation `shadow`/`primary` rollout

`RICHMOD_DOCUMENT_INTERPRETATION` is empty in production (P0 audit), so the
worker runs `legacy`. `primary` is compiled off
(`primaryInterpretationEnabled = false`). Only `shadow` is selectable, and it
persists nothing that any feature reads; it only writes comparison telemetry.
Footprint: `document/intelligent.go` (430 lines), `document/shadow.go`, the
shadow block in `document/processor.go:155-225`, and `primary_test.go`,
`shadow_test.go`, `integration_interpretation_test.go`. `repair.go` is **live**
(used by legacy extraction) and is not part of this.

Options: (1) **remove** shadow + primary and supersede ADR-037 with a short ADR
stating legacy extraction plus one-pass repair is the design, or (2) keep it
for a future rollout. The P0 audit kept it because ADR-037 was still the
current contract; the release is the moment to decide.

## 8. F — Comments ("AI slop")

Production comments in 55 files cite delivery history instead of explaining
the code:

| Pattern | Production | Tests | Example |
| --- | --- | --- | --- |
| Sprint/slice tags `CEU-`, `SAVR-`, `UIR-`, `UIRC-`, `IR-`, `UISC-` | ~70 | ~50 | `// (SAVR-06).` |
| Review-bot rounds | 15 | 17 | `(SAVR-06, Hermes round 4)`, `(Hermes B1)` |
| `PRD §n` references | ~147 | ~67 | `// PRD §22 product scoreboard` (ambiguous: there are ~15 PRDs) |
| Header lines naming the sprint that created the file | several | — | `// Cycle residual reconciliation (UIR-01).` |

Rule for the rewrite:

- Delete parenthetical tags `(SAVR-06)`, `(CEU-02)`, `(Hermes round 4)`, and
  the sprint prefix on headers. Keep the sentence if it still explains *why*.
- Replace `PRD §n` with the actual rule in one clause, or drop it if the
  sentence already states the rule.
- Keep `ADR-nnn` and `BDR-nnn` references: those are durable decisions.
- Delete comments that only narrate what the next line does or record history
  ("previously…", "now…", "fixed…").
- Test names and test comments follow the same rule; rename
  `savr07_contract_test.go`, `*_prd_test.go`, `prd_acceptance_*` to say what
  they test.

This is comment-only; `go build` and `go vet` prove it changes no behaviour.

## 9. G — Tests that freeze source text — **DECIDE scope**

**Web.** Several test files read component and CSS source and regex-match
literal strings. That fails on harmless refactors and passes on broken UI.

| File | Tests | Regex-on-source asserts |
| --- | --- | --- |
| `ui-audit-locks.test.mjs` | 44 | 61 |
| `cycle-ledger.test.mjs` | 24 | 46 |
| `review-proposal-first.test.mjs` | 12 | 38 |
| `product-alignment.test.mjs` | 27 | 37 |
| `cycle-review.test.mjs` | 14 | 34 |
| `calendar-review.test.mjs` | 5 | 19 |
| `ui-sprint-7-locks.test.mjs` | 4 | 6 |
| `ui-sprint-8-locks.test.mjs` | 4 | 6 |

Examples of low-value asserts: `assert.match(css, /\.settings-index a \{
min-width: 32px;/)`, `assert.match(inbox, /wealthResponse, accountResponse/)`.

Keep the tests that compute something real: WCAG contrast over the colour
tokens, "every worker document type has an Indonesian label", the ADR index
generator, pure-function tests (`chart-data`, `insight-data`, `calendarReview`,
`cycleLedger`), and `imports.test.mjs`. The visual-smoke baselines already
guard layout.

Recommendation: delete assertions that match literal markup/CSS/variable
names, keep behaviour and contract checks, fold the two `ui-sprint-*` files
into `ui-audit-locks.test.mjs`, and rename by subject.

**Go.** Ten files read `.go` source. Two kinds:

- *Architecture guards* (keep): "no adapter owns `INSERT INTO salary_event`",
  "Web and Telegram call the shared `reviewdomain` operation"
  (`salary_contract_test.go`, `confirm_contract_test.go`,
  `wealth_contract_test.go`, `reject_contract_test.go`,
  `duplicate_contract_test.go`, `cycle_contract_test.go`). They protect an
  AGENTS.md rule, not a spelling.
- *Exact-line pins* (rewrite or drop): `judgment_fast_path_test.go:60` pins a
  literal `if` statement; `payslip_test.go:123` counts a call string;
  `savr07_contract_test.go` checks for `"sameFacts"`. Replace with a
  behavioural test where one exists, otherwise delete.

## 10. H — Historical documentation — **DECIDE**

`docs/` has 70 top-level Markdown files; `docs/README.md` already says most are
historical. Proposed: move every top-level PRD, `*_CHECKLIST.md`, `*_CODEX.md`
brief and dated verification record that `docs/README.md` does not link as
current into `docs/archive/`, update links, and keep `DATABASE_SCHEMA.md`,
`brand-guidelines.md` and the current product/analytics contracts at the top
level. Nothing is deleted. (Comment rewrites in section F remove the code's
dependence on PRD section numbers, which makes this move safe.)

## 11. Retained deliberately

| Item | Reason |
| --- | --- |
| `processBoundReview` and its typed branches | The P0 audit listed it as the next candidate, but `agent.go:70` now calls it on the live agent path. |
| Env-gated test skips (`TEST_DATABASE_URL`, live LLM/OSS smoke, canary corpus) | Intended opt-in gates; CI supplies the database. |
| `reviewdec` vocabulary constants | Closed, documented vocabularies; see section 4. |
| Historical migrations, including job types from section 5 | Migrations are immutable. |
| SAVR-10 retained compatibility paths | Still guarded by open historical rows; see `SAVR-10-legacy-retirement.md`. |

## 12. Proposed execution

One branch per slice so each PR stays reviewable and revertible:

1. **Mechanical dead code** — sections A, B (removals), C, D. No behaviour
   change. Verify with `go vet`, `go test ./...` (with `TEST_DATABASE_URL`),
   the `deadcode`/`staticcheck` re-run (expected: only the kept seams),
   `npm test`, `next build`, visual smoke.
2. **Comment cleanup** — section F. Comment-only diff plus test renames.
3. **Test cleanup** — section G, at the scope you approve.
4. **Dead features** — section E, only the options you approve, with ADR
   updates (supersede ADR-014 / ADR-037 as decided) in the same branch.
5. **Docs archive** — section H, if approved.

Each slice re-runs the analysers from section 1; the release tag is cut when
they report only the items in section 11.

## 13. Execution record

Owner decisions (2026-10-03): remove budgets (E1); keep void, confirm and
reverse-merge and remove the other uncalled routes (E2); remove the
`shadow`/`primary` interpretation stages (E3); clean tests as suggested (G).
Section H (docs archive) was not decided and is **deferred**.

| Slice | Result |
| --- | --- |
| Dead code (A-D) | Removed as listed. `ActiveReasons`, `NewCycleHandler` and `FailedSourceInboxLink` turned out to be cross-package or contract test seams and were kept. `TenantContext` and `ErrHouseholdRequired` became dead with `TenantFromPrincipal` and were removed. `document.Handler` lost its unused `root` field and lazy local-storage fallbacks. The two Telegram test wrappers moved into their test files. |
| Dead features (E) | ADR-051 records the retirement; ADR-014 superseded, ADR-037 partially superseded. `RICHMOD_DOCUMENT_INTERPRETATION` removed from `.env.example` and Compose. `sanitizeEvidenceText` (still used by payslips) moved to `payslip.go`. A test that asserted an otherwise unused `maxRepairAttempts` constant was removed with it. No migration. |
| Comments (F) | No sprint tag, PRD section number or review-round reference remains in Go or JS. Sprint/PRD-named test files and functions renamed by subject. `CEU` stays where it is a persisted name (`CEU_BINDING`, `ceuBinding`). |
| Tests (G) | Two sprint lock files folded into `ui-audit-locks.test.mjs`; pure CSS/markup pins removed; accessibility, security, role, contract and computed checks kept. The fast-path period guard became `routeConsumesPeriod`, tested over every route. |

After the cleanup, `deadcode` (production) lists only the five kept seams
(`ContextWithPrincipal`, `NewCycleHandler`, `TelegramCallbackSamples`,
`ActiveReasons`, `AgentFinanceTools`); `deadcode -test` and `staticcheck` U1000
report nothing.
