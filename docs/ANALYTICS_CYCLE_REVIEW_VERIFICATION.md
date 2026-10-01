# Cycle-review verification evidence

Current follow-up: PR241 calculation/cutoff audit after the PR230–240 contract.
Exact-head CI/automatic-review/merge gates remain pending until they pass;
this file records the checks, not an unobserved production deployment.

## Drift guard A–R

| Guard | Evidence and verification boundary |
| --- | --- |
| A — household review purpose | `/analytics` cycle outcome, changes, drivers, savings/Wealth, quality, discussion, decisions; browser smoke follows all eight closed-cycle meeting steps without AI. No budgeting feature. |
| B — deterministic ownership | `analyticscore.Load` performs exact Go/SQL arithmetic in a repeatable-read read-only transaction. API/worker/browser tests cover refunds, transfers, medians, proportions, elapsed dates and Wealth. Browser formats/layouts values only. |
| C — baseline integrity | `TestCycleReviewHistoryAvailabilityAndActiveElapsedDays`, `TestMedian3UsesMiddleCompletedCycle`, `TestChangeSeparatesPreviousCycleFromRecentMedian`, `TestRelativeChangeRetainsAbsoluteTinyDenominatorContext`; browser outlier row displays previous and median separately. Missing history stays null. |
| D — analytical ownership | `facts.go` computes objective change/order; worker `prompt` assigns interpretation to intelligence. `TestReviewDoesNotAuthorSemanticConclusions` rejects narrative/importance fields. No semantic threshold tree or Go no-noteworthy predicate. |
| E — native data boundary | `TestAnalyticalArgsAreStrictAndReadOnly`, `TestAnalystRejectsUnexposedUnsafeAndMalformedBatchBeforeReads`; script `check_native_only_llm.sh`; no new gateway path. |
| F — data access/privacy | `TestAnalyticalDependentReadsAndPrivacy` checks every READ projection for canonical IDs, household IDs, raw evidence and account membership leaks; unissued refs fail, dependent READs are model-selected. Display names/amounts intentionally remain available for reasoning. |
| G — presentation | UI renders facts independently of plain commentary; `TestRenderingOnlyCarriesNaturalProse` and native-loop tests prohibit prose/JSON domain parsing. Gateway outage preserves deterministic review. Telegram stays ADR-033 natural text. |
| H — no Go pseudo-intelligence | `TestReviewDoesNotAuthorSemanticConclusions`, native-only guard, worker failure tests and source inspection. No keyword/regex/threshold/template analyst; Go error/protocol strings are not analysis. |
| I — charts | Daily shape, category movement/distribution and merchant drivers answer explicit review questions. Selected period/cutoff/baselines/IDR tooltip/empty state/accessibility/drill-down are checked by four-width synthetic browser smoke. No future zeros. |
| J — composition | Existing Richmod tokens and document sections, dominant net cashflow, proportional movement table, descriptive attribution, supporting-data actions. CI screenshots at 1440, 1024, 390, 320 widths; failed facts never render zero financial data; failed commentary stays local. Keyboard headings/controls and no-overflow checks. |
| K — neutrality | Worker prompt prohibits blame, scores, motives and advice. Attribution is descriptive. Decisions have no model tool; browser save requires explicit human action. No causal success claim. |
| L — household decisions | `TestCycleDecisionsExplicitSaveAuditAndHouseholdIsolation` checks author binding, active membership, prior-cycle context, same-transaction audit, denied foreign access, soft revocation, no financial rows. Invalid input tests and browser retained-draft/explicit retry/revoke tests. Telemetry never copies text. |
| M — quality | Facts expose Inbox/settings/Wealth/category blockers, not model confidence. `TestCompletenessUsesGrossExpenseAndReviewCoverage` and worker data-failure tests enforce existing coverage suppression; no missing evidence guessed or retry loop on insufficient facts. |
| N — persistence | `cycle-review-v1`, `cycle-analyst-v4`, deterministic request snapshot and executed native READ transcript retained in `insight`; historical rows preserved, legacy jobs explicitly superseded. Schema through 00074 matches decision entity/ERD; calculation/cutoff fixes change no schema. Model prose never mutates financial state. |
| O — regression | Shared facts/tool tests, PostgreSQL API/worker integration, model phase/read-count/order tests, web tests and synthetic Playwright smoke. Stable/no-filler, previous outlier, large category/transaction drivers, tiny denominator, incomplete state, AI outage, correct ledger binding and cross-household decisions. No acceptance assertion weakened. |
| P — scope | No budgets/goals/recurring/portfolio/live prices/new infrastructure/provider branch/Telegram redesign; no Python, Redis, Kafka or direct provider integration. |
| Q — review questions | Answers below; PR describes the evidence and limits. |
| R — Telegram reuse | `TestTelegramAnalyticalQuestionsReuseCanonicalEngineAndNaturalReply` covers the five representative question classes; `TestTelegramExposesSharedAnalyticalReadTools` checks the common catalog. Same engine/native READs, existing bounded loop, no parallel SQL/financial math or response-protocol redesign. |

## PR review answers

1. The household can finish a reliable closed-cycle review, including when AI is
   unavailable, without reconstructing a spreadsheet.
2. Sprint 5 adds no financial facts. Existing facts remain computed in
   `analyticscore`; this sprint fixes observability, retrieval and documentation.
3. Previous completed comparable cycle plus three-completed-cycle median where
   available; active cycles compare elapsed days, not future periods.
4. Generative intelligence judges open-ended noteworthiness. Go only computes
   exact amounts, deltas, ratios, ordering and supported-data coverage.
5. Eight shared READ tools, documented in `ANALYTICS_SHARED_READ_TOOLS.md`.
6. Authenticated Web API/commentary worker and existing Telegram agent reuse them.
7. Bounded facts/names/amounts; no canonical IDs, credentials, raw email/document
   evidence, unrestricted SQL/ledger dumps or side effects.
8. Deterministic section tables/charts, supporting transactions and server-bound
   ledger/Wealth/Inbox links. The model never creates a drill-down URL.
9. Commentary fails locally; facts, comparisons, drivers, charts, savings, Wealth,
   quality and explicit household decisions remain usable.
10. Strict schemas, tool allow-list, bounded native-loop tests, privacy tests and
    the native-only CI guard prevent JSON-in-text and duplicate analysis drift.
11. `TestAnalystNativeDependentReadsNoFillerOrAdviceContract` proves concise model
    output/read order; the stable browser fixture proves one paragraph is rendered
    without invented findings or automatic generation.
12. `TestChangeSeparatesPreviousCycleFromRecentMedian`, PostgreSQL baseline tests
    and the browser outlier row assert separate previous/recent-median context.
13. The entire financial review, meeting navigation and explicit decisions.
14. Sprint 5 creates no new authority. Sprint 4 human-authored notes are distinct,
    auditable household state; Go exclusively owns canonical financial mutations.

## Hardening-specific checks

Events have field allow-lists and are emitted only after successful reads/commits;
denials add no success event. They count API acknowledgements, not unique users
or durable exactly-once delivery. No prose classifier is added for no-noteworthy
telemetry. Query/payload/waterfall/rate-limit and correction/reuse semantics are
documented in `ANALYTICS_CYCLE_REVIEW_TELEMETRY.md`; no production benchmark claim.

README capture now uses shared synthetic facts and plain non-advisory commentary.
`readme-analytics.png` is generated by CI at 1440×1050, visually inspected before
updating `docs/assets/analytics.png`. Historical banners preserve old execution
records without making their point-in-time UI requirements current again.

## Gates

October 1 calculation audit: local Node tests 80/80; initial PR #241 CI passed
API/shared-domain/worker integration and vet, web build/tests, four-width browser
checks, containers, secrets and CodeQL. Automatic review found cutoff reuse
needed server enforcement. The revision adds exact-cutoff reuse plus stale
pending/recent-success/expired-success regressions while preserving the hourly
cap. The exact revised head must pass CI and automatic review before merge.

- Historical Sprint 5 local web tests (superseded, pre-revision): diff whitespace and native-only
  guard passed. Current calculation/cutoff revision: 81 Node tests passed.
- Initial Sprint 5 CI: frontend/browser, containers, secrets and CodeQL passed.
  Backend found a new fixture's missing required author, then its missing
  completion timestamp for `SUCCEEDED`; both fixed without weakening assertions
  or database constraints. Automatic review reported the author blocker.
- Exact-head full CI and automatic review: required before merge.
- Exact merged main CI and four immutable release images: required before reclaim.
- Deployment: not requested. No production data, restart or environment approval
  used for any verification above.
