# SAVR-09 — Corpus, Canary, and Product Metrics

**Status:** product-complete (UISC-04, 2026-10-03); UISC-01/02 merged into
`main@c5278a4`; owner-household observation accepted by the product owner

**Date:** 2026-09-28
**Method:** disposable PostgreSQL 17.4, goose to migration 72, then
`go test ./...` and `go vet ./...` in `apps/api` and `apps/worker`.

Integration tests skip when `TEST_DATABASE_URL` is absent, so a bare module run
is not evidence. The corpus results below come from the full matrix. Every named
test was re-checked against the source; an earlier draft of this report cited a
test name (`TestBankEmail*Fallback`) that does not exist, so treat the table as
the only authoritative list.

---

## 1. Corpus coverage

| Required case | Test | Result |
| --- | --- | --- |
| Synthetic ATI payslip / first-salary policy | `TestPayslipQualityAndFirstSalaryPolicy`, `TestPayslipReviewSeparatesDateAndSalaryPolicy` | pass |
| Payslip representation (real payroll range) | `TestPayslipRepresentationAcceptsRealPayrollRange` | pass |
| Payslip one extraction, no Jev replay | `TestPayslipUsesOneGenerativeExtractionAndNoJevReplay` | pass |
| Payslip invalid machine output | `TestPayslipInvalidMachineOutputDoesNotAskHousehold` | pass |
| Payslip duplicate/conflict parity | `TestPayslipFinalizerLinksExactDuplicateAndRejectsConflict` | pass |
| Payslip autonomous/reviewed share one finalizer | `TestPayslipPathsShareFinalizer` | pass |
| Payslip missing date lists only real residual | `TestPayslipMissingDateListsOnlyCurrentResidualAndPolicy` | pass |
| Screenshot missing amount | `TestScreenshotMissingAmountReviewFinalizesWithoutSentinel`, `TestScreenshotMissingAmountCanBeIgnoredWithoutInventingAmount` | pass |
| Screenshot clear rows need no review | `TestScreenshotBatchWithOnlyClearRowsNeedsNoReview` | pass |
| Cross-source merchant memory | `TestLoadMerchantMemoryRequiresOneUnambiguousNormalizedRule`, `TestBankEmailB1LearnedMerchantConfirmsWithoutReview`, `TestBankEmailB1LearnedMerchantCreatesNoReviewWork` | pass |
| Receipt arithmetic quality | `TestReceiptArithmeticMismatchConfirmsPrintedTotalWithoutReview`, `TestReceiptR2StrongMatchLinksEvidenceWithoutDuplicate` (follow-up branch) | pass |
| Bank single-predicate disagreement | `TestBankEvidenceVerificationIsPersistedForAnUnsupportedRuling`, `TestEvidenceVerificationSupportsLowExtractorConfidence`, `TestBankEmailB5UndecidedAmbiguityDoesNotAuthorize` | pass |
| Bank schema failure is not human work | `TestBankSchemaFailureDoesNotCreateHumanReview` | pass |
| Bank `EMAIL_RECEIVED_AT` fallback | `TestApplyEmailReceivedTimeFallback` (worker policy) | pass |
| Provider-email exact residual / no canonical write | `TestEvidenceReviewParksProviderFactsWithoutCanonicalWrite`, `TestFinancialEmailProviderReferenceSemanticConflictsRequireReview` | pass |
| Provider-email account/hint materiality | `TestSelectedAccountConflictingSourceHintFailsClosed`, `TestFinancialEmailWithoutConfiguredDefaultUsesUniqueHint` | pass |
| Payment-mechanism materiality | `TestBankEmailB5DecidedNotAmbiguousAuthorizes`, `TestBankEmailB5UndecidedAmbiguityDoesNotAuthorize` | pass |
| Telegram batch explicit confirmation | `TestPendingBatchConfirmationUsesHumanAuthority`, `TestAgentPendingBatchConfirmationUsesHumanAuthority`, `TestPendingBatchConfirmationRejectsMissingCategory` | pass |
| User correction | `TestManualCorrectionReviewCompletesFromBoundReply` | pass |
| Natural Indonesian date | `TestParseReviewPayDate`, `TestTelegramPayslipPolicyAndDateResolveWithoutWeb` | pass |
| Product telemetry + auto-confirm correction rate | `TestProductTelemetryCapturesTurnAndAutoConfirmCorrection` | pass |
| Known-fact re-ask only with a decision contract | `TestProductAggregateCountsOnlyKnownFactReasksWithContracts` | pass |

---

## 2. Metrics

| Metric | Status |
| --- | --- |
| RHICE | Measured — derived from canonical-cohort review turns in `apps/api/internal/operations/product.go` |
| Known Fact Re-ask Rate | Measured — from ReviewDecision `knownFacts`/`missingFacts`; only contracts carrying `missingFacts` are counted |
| Calls per event | Counts available from `intelligence_phase_telemetry` (`passes` and `events` in Operations); no explicit derived rate field |
| Auto-confirm correction rate | Measured — `autoConfirmCorrectionRate` from `product_telemetry_event` `AUTO_CONFIRM_CORRECTION` turns joined to auto-confirmed transactions |
| Validator-Induced Human Review Rate | **Closure instrumentation gap** — accepted-dimension ↔ validation-consequence provenance must become queryable for eligible new rows |
| Residual Contract Fidelity | **Closure instrumentation gap** — supersedes labelled-ground-truth Residual Fidelity; derive structurally from ReviewDecision + resolution contract |
| Semantic Re-decision Rate | **Closure instrumentation gap** — intelligence phases must expose accepted-at-entry or equivalent re-decision provenance |

The three closure gaps remain surfaced rather than reported as zero until
UISC-02 lands. Existing `ReviewDecision`, `product_telemetry_event`,
`judgment_decision`, and `intelligence_phase_telemetry` are the preferred
sources. Historical rows without new provenance remain coverage-incomplete and
must not be coerced to zero.

**Not claimed:** a deployed before/after canary. Deployment alone does not prove
that SAVR improves interaction/semantic efficiency without worsening correction
rate. What the corpus proves is narrower: the
changed paths no longer create human work for machine-only failures, and known
facts survive validation.

---

## 3. Production observation gate — owner household is the canary

The previous disposable-household requirement is superseded by
`docs/archive/RICHMOD_UIR_SAVR_CLOSURE_PRD.md` / BDR-005.

Richmod is currently a personal production system with one real household. The
production observation cohort is therefore the actual owner household using the
product normally.

Rules:

- do not seed fake transactions, fake emails, fake members, or synthetic
  financial state into production;
- continue normal Telegram/email/document usage;
- use real review/correction history and Operations metrics as product evidence;
- use disposable PostgreSQL/integration fixtures for deterministic edge-case
  testing;
- source families that do not naturally occur may be marked
  `PRODUCTION_UNOBSERVED` when their corpus remains green;
- newly instrumented metrics are measured prospectively when historical
  provenance cannot be reconstructed honestly.

Four existing kill switches let bounded auto-confirm behavior be reverted
without a deploy (all on by default; set one to `0`, `false`, `off`, `no`, or
`disabled` to disable it):

| Switch | Governs |
| --- | --- |
| `RICHMOD_AUTOCONFIRM_SCREENSHOT` | screenshot row auto-confirm |
| `RICHMOD_AUTOCONFIRM_RECEIPT` | receipt auto-confirm |
| `RICHMOD_AUTOCONFIRM_BANK_CATEGORY` | bank category auto-confirm |
| `RICHMOD_AUTOCONFIRM_TELEGRAM` | post-generative Telegram auto-confirm |

Unified document interpretation stays off unless separately approved.

---

## 4. Exit

Corpus regressions pass, and no SAVR change worsened a correction path measured
so far: the SAVR-07 payslip lane keeps duplicate/conflict safety, SAVR-08
removed a machine-only review, and S08-09 treats a complete receipt's component
mismatch as quality metadata. UISC-02 observability instrumentation is merged
at `c5278a4` and the three metrics are now prospectively measurable: validator-
induced review, semantic re-decision, and Residual Contract Fidelity. Historical
rows lacking required provenance remain unknown/coverage-incomplete, never
synthetic zero. **Open exit item:** owner-household production observation and
product-owner acceptance. SAVR-09 remains
*corpus-complete* until the combined closure gate was accepted. **UISC-04
(2026-10-03):** the product owner accepted the owner-household observation recorded
in `CORE-INTELLIGENCE-BOUNDARY-REPAIR.md` (Telegram fallthrough and a canonical
READ observed; no human work created by route uncertainty). Email-origin review
projection and the Jago/Bibit rechecks were not naturally observed and stay
`PRODUCTION_UNOBSERVED` with green corpus evidence, as BDR-005 permits. SAVR-09
is therefore **product-complete**. Any later production defect is handled
against the approved SAVR contract, not by reopening it.
