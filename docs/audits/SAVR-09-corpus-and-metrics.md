# SAVR-09 — Corpus, Canary, and Product Metrics

**Status:** corpus measured; production deployed at `64a6195` on 2026-09-28;
**canary not run — disposable household still needed**

**Date:** 2026-09-28
**Method:** disposable PostgreSQL 17.4, goose to migration 71, then
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
| Validator-Induced Human Review Rate | **Coverage gap** — Operations `notYetMeasurable` (`validator_induced_review_consequences`); needs accepted-fact ↔ consequence provenance no writer records yet |
| Residual Fidelity Rate | **Coverage gap** — `notYetMeasurable` (`residual_fidelity_ground_truth`); needs a labelled ground-truth residual set, not derivable from canonical state alone |
| Semantic Re-decision Rate | **Coverage gap** — `notYetMeasurable` (`semantic_redecision_accepted_fact_provenance`); needs accepted-fact provenance across layers |

The three coverage gaps are surfaced by the Operations aggregate rather than
reported as zero. No new telemetry table was added: the existing
`product_telemetry_event`, `judgment_decision`, and
`intelligence_phase_telemetry` are the sources.

**Not claimed:** a deployed before/after canary. Deployment alone does not prove
that SAVR improves interaction/semantic efficiency without worsening correction
rate. What the corpus proves is narrower: the
changed paths no longer create human work for machine-only failures, and known
facts survive validation.

---

## 3. Canary — not run

The SAVR-09 exit criterion (a measured before/after correction-rate canary)
requires a deployed build against a **disposable household** with live
Telegram/email traffic. Production was deployed with user approval on
2026-09-28; the canary remains open until a separate household and traffic
are available. Do not seed test financial data into the real household.

Four existing kill switches let any bounded auto-confirm behavior be reverted
without a deploy (all on by default; set one to `0`, `false`, `off`, `no`, or
`disabled` to disable it):

| Switch | Governs |
| --- | --- |
| `RICHMOD_AUTOCONFIRM_SCREENSHOT` | screenshot row auto-confirm |
| `RICHMOD_AUTOCONFIRM_RECEIPT` | receipt auto-confirm |
| `RICHMOD_AUTOCONFIRM_BANK_CATEGORY` | bank category auto-confirm |
| `RICHMOD_AUTOCONFIRM_TELEGRAM` | post-generative Telegram auto-confirm |

Unified document interpretation stays off. A canary must use these switches and
a disposable household; it must not touch production financial data.

---

## 4. Exit

Corpus regressions pass, and no SAVR change worsened a correction path measured
so far: the SAVR-07 payslip lane keeps duplicate/conflict safety, SAVR-08
removed a machine-only review, and the follow-up S08-09 fix treats a complete
receipt's component mismatch as quality metadata. **Open exit items:** the
deployed canary and the three coverage gaps above. SAVR-09 is therefore
*corpus-complete*, not *product-complete*.
