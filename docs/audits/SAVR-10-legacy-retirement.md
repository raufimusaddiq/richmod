# SAVR-10 — Legacy Reinterpretation Retirement

**Status:** corpus-proven removals complete on this branch; freeze is
*conditional* on the SAVR-09 canary  
**Date:** 2026-09-28

SAVR-10 removes only what the corpus proves unreachable. Everything retained
below has a stated reason. Because the deployed canary has not run, this is a
**partial** freeze: the semantics below are frozen, but further reduction should
be driven by measured canary data, not by guessing.

---

## 1. Removed (corpus-proven)

| Path | Evidence | Change |
| --- | --- | --- |
| Payslip invalid extraction → `DOCUMENT_EXTRACTION_LOW_CONFIDENCE` review | `TestPayslipInvalidMachineOutputDoesNotAskHousehold` | replaced by the shared `persistInvalidDocumentExtraction` machine-failure writer |
| Payslip generic `MANUAL_CORRECTION` lane | `TestPayslipQualityAndFirstSalaryPolicy`, `TestPayslipPathsShareFinalizer` | SAVR-07 routes both autonomous and reviewed slips through `FinalizePayslip` |
| Duplicate payslip delete-then-relink transaction surgery | `TestPayslipFinalizerLinksExactDuplicateAndRejectsConflict` | the finalizer links evidence to the existing transaction only when amount and pay date agree; a conflict fails closed |

## 2. Retained deliberately

| Path | Reason |
| --- | --- |
| Bank evidence verification (Jev) | independent evidence support for untrusted email facts (ADR-045) |
| Bank `EMAIL_RECEIVED_AT` fallback | system-derived policy fact, not human work (BDR-004) |
| Receipt/screenshot duplicate candidate guards | duplicate safety; only materially plausible candidates raise review |
| Receipt/screenshot category rescue | bounded residual only; undecided or failure keeps review |
| Provider `TRANSFER_CLASSIFICATION` recovery lane | still used for missing amount/time/account, undecided movement semantics, or incompatible transfer purpose; see `planCash` |
| Legacy payslip review rows without document binding | historical open state; `ResolvePayslipProposal` still accepts them behind an explicit match |
| Legacy `RECEIPT_MISMATCH` reviews | historical open rows and the receipt kill-switch still use this compatibility path; the default complete-facts path no longer produces it |
| Email-origin review projection via originating chat | compatibility only; household-recipient projection is a UIR defect (SAVR-08 S08-08) |

## 3. Not removed because not proven unreachable

- `unresolvedTransactionDimensions` in the Telegram single-record path: the
  corpus proves the batch lane no longer replays semantics, but the single-record
  residual vocabulary is still the routing key for date/category prompts.
  Removing it would need a replacement residual contract.
- Compatibility review reasons (legacy document reasons): historical open rows
  still reference them, and the render gate must stay fail-closed for them.

Deleting either without a corpus-proven replacement would trade a known state
for an untested one, which SAVR-10 forbids.

## 4. Freeze

SAVR semantic authority is frozen **within this branch** as:

```text
accepted facts survive validation
residuals name only material blockers
machine failure is not human work
human input is policy or irreducible fact only
```

## 5. Open items before a full freeze

- SAVR-09 live canary in a disposable household (deployment approved and completed 2026-09-28);
- the three Operations coverage gaps
  (`validator_induced_review_consequences`, `residual_fidelity_ground_truth`,
  `semantic_redecision_accepted_fact_provenance`);
- the adjacent UIR email-origin projection defect (S08-08), tracked outside SAVR.

Next initiative: CEU.
