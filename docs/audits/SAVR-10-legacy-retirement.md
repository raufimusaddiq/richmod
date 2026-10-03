# SAVR-10 — Legacy Reinterpretation Retirement

**Status:** FULL FREEZE (UISC-04, 2026-10-03); corpus-proven removals complete;
UISC-01/02 merged (`c5278a4`); owner-household observation accepted.
**Date:** 2026-09-28

SAVR-10 removes only what the corpus proves unreachable. Everything retained
below has a stated reason. UISC-01 restored household-owned email review
projection and UISC-02 made the three SAVR closure metrics measurable. The
**full freeze** remains conditional on real owner-household production
observation and acceptance. Further reduction must be driven by measured
real-use evidence, not by synthetic production traffic or guessing.

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
| Email-origin review projection via originating chat | **resolved by UISC-01**; both email producers now project through the universal household-recipient resolver, and the projector skips closed or document-only reviews that cannot be completed in Telegram |

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

## 5. Closure status

- UISC-01 household-owned bank/financial-email review projection — merged
  (closes S08-08);
- UISC-02 Operations observability for validator-induced review, Residual
  Contract Fidelity, and semantic re-decision — merged, with old rows kept
  coverage-incomplete rather than zero;
- UISC-03 owner-household production observation under BDR-005 — **accepted by
  the product owner on 2026-10-03**, using normal real usage and no seeded
  production financial data; email-origin projection and Jago/Bibit rechecks
  remain `PRODUCTION_UNOBSERVED` with green corpus evidence;
- UISC-04 final docs freeze — **done** (this change).

```text
UIR frozen
SAVR frozen
owner-household production observation accepted
CEU may start
```

UIR and SAVR change from here only for production defects against their
approved contracts. Next initiative: CEU.
