# SAVR-08 — Source-Family Parity and Legacy Isolation

**Status:** audit complete on this branch  
**Date:** 2026-09-28  
**Baseline:** `a50ecd4` (post-SAVR-06 reconciliation) plus the SAVR-07 payslip
finalizer on `feat/savr-07-payslip-continuity`.

This audit answers one question per boundary:

> Can Richmod reach the canonical outcome without this AI call or this human
> input, and if not, is the remaining work material?

A boundary is not parity-complete because its residual is exact. The residual
must also be material, and the reviewer must be able to supply it.

---

# 1. Materiality contract

An input (AI call or human review) is material only when it can change the
attempted canonical outcome, satisfy a hard canonical invariant, resolve an
independent evidence conflict, or obtain irreducible household policy.

Non-material inputs — generic confidence, an unproven payroll breakdown, a
redundant prose hint, a machine/schema failure — must not raise RHICE.

---

# 2. Boundary matrix

| Boundary | Attempted outcome | Material dimensions | Non-material | Minimum sufficient path | Extra AI call? | Human input? | System-derived facts |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Bank email (SPENDING_ONLY) | CONFIRMED expense | amount, direction, transaction time | QR/debit mechanism, prose account hint | LLM extract → Go policy; Jev only for independent evidence support | Jev is the fixed evidence check, not a re-read | none when evidence supported and merchant known | `transaction_at = received_at` when the email prints no time (`EMAIL_RECEIVED_AT`) |
| Bank email, evidence unsupported | review with exact failed predicate | the failed predicate only | other verified facts | — | no | one bounded choice naming that predicate | household merchant memory resolves category |
| Bank email, schema invalid | REPAIR/FAILED machine state | none | all | — | no extra call; extractor already retried | none — not a household question | operator retry owns it |
| Financial provider email | CONFIRMED/UNCLASSIFIED movement | amount, time, movement type, account | prose account hint | LLM extract → Go planner; Jev classification when configured | Jev is a material evidence check only | entity binding only when Go cannot resolve | `resolutionGaps` names unresolved entities |
| Receipt | CONFIRMED expense | amount, printed date, category, arithmetic when available | generic confidence | LLM extract → Go validate; Jev only for an undecided category | category rescue is bounded | none when a category resolves | merchant memory before Jev |
| Receipt, arithmetic mismatch | CONFIRMED expense, or evidence link to one strong match | printed total/date/category | component arithmetic is quality metadata | Go uses printed total and persists `arithmetic_ok=false` | no | none when facts complete and no duplicate ambiguity | total/date/category stay known |
| Screenshot row | CONFIRMED expense/income | amount, date, category, direction | generic confidence | LLM extract → Go validate → merchant memory | Jev only for an unresolved category | amount/date only when genuinely absent | merchant memory |
| Screenshot row, no amount | review `MISSING_AMOUNT` | amount | everything else | — | no | typed amount | date/category/merchant stay known |
| Payslip | CONFIRMED income + salary event | net pay, employer, period, pay date | extraction confidence, unreconciled breakdown | LLM extract → Go validate → shared salary finalizer | none | first-source policy or missing pay date only | caption date before validation; owner as salary source user |
| Payslip, invalid extraction | FAILED machine state | none | all | — | no extra call; repair already bounded to one | none — not a household question | operator retry owns it |
| Telegram single record | canonical transaction | amount, category, date | — | deterministic acceptance, else Jev residual only | Jev only for a named residual | typed user facts | merchant memory |
| Telegram staged batch | N canonical transactions | structural/canonical invariants | all semantics already shown | Go only | none after explicit CONFIRM | explicit CONFIRM | active household category check |
| Cycle residual | allocation metadata | allocation choice | — | Go + human policy | none | one policy choice | retained balance is computed |
| Wealth observation | snapshot | account, value, date | — | Go + human confirmation | none | one bounded account/value confirmation | — |

---

# 3. Findings and dispositions

## S08-01 — Payslip invalid extraction created human work — FIXED

An unrepairable payslip extraction wrote `NEEDS_REVIEW` and opened
`DOCUMENT_EXTRACTION_LOW_CONFIDENCE`, even though the household cannot supply
malformed model output. Receipt and screenshot already used
`persistInvalidDocumentExtraction`, which records `FAILED`/unvalidated
extraction and leaves the event for operator repair.

**Change:** the payslip invalid path calls the same shared failure writer.
The unused payslip-specific review writer was deleted. The regression invokes
`ProcessPayslip` with invalid extraction and a failed bounded repair; it asserts
both `FAILED` states, one unvalidated extraction, zero household reviews, and
exactly two model calls (extract + one repair).

**Classification:** MACHINE_ONLY_FAILURE_AS_HUMAN_WORK — removed.

## S08-02 — Bank schema-invalid state — ALREADY CORRECT

`SchemaError` persists `INVALID / REPAIR`; provider verification outage
persists `VERIFICATION_FAILED / RETRY`. Neither opens a review.

**Classification:** HEALTHY.

## S08-03 — Bank payload account hint — HEALTHY

`planCash` requires either a selected canonical account or a source hint, so a
completely unknown account still gets the human recovery lane. A selected
canonical account survives a missing hint; a resolvable contradictory hint
becomes an explicit account conflict. This matches post-SAVR-06 D3/D4.

**Classification:** HEALTHY.

## S08-04 — Receipt and screenshot confidence — HEALTHY

Neither path gates on generic extraction confidence. Receipt confirms on known
category/date/no-candidate and consistent arithmetic; screenshot rows confirm on
accepted material facts while duplicate candidates still block.

**Classification:** HEALTHY.

## S08-05 — Duplicate candidate materiality — HEALTHY

`findMatches` keeps a same-amount candidate plausible only for an exact merchant
within 72 hours or any same-amount transaction within one hour; a
different-merchant 12–72 hour query hit is not presented as ambiguity.

**Classification:** HEALTHY.

## S08-06 — Provider email evidence bundle — HEALTHY

`evidence_sufficient` checks material value/amount and, for cash movements,
transaction time. It does not require the prose account hint, and no additional
intelligence pass was introduced to split the bundle.

**Classification:** HEALTHY.

## S08-07 — Telemetry dimension arrays — HEALTHY

`recordIntelligencePhase` normalizes nil `Dimensions`, `AnsweredDimensions`,
and `ResidualDimensions` to empty arrays before the insert explicitly supplies
them, so the NOT NULL contract holds without a schema change.

**Classification:** HEALTHY.

## S08-08 — Email-origin review projection — CLOSED IN MAIN (UISC-01)

Bank and provider-email projection helpers still derive Telegram delivery only
from Telegram-origin source payloads, so an email-origin review can stay
Inbox-only even when an eligible recipient exists.

This is a UIR projection defect, not SAVR semantic authority. Fixing it here
would mix review-delivery scope into a semantic sprint.

**Classification:** TRACKED_SEPARATELY — belongs to UIR, not SAVR-08.

## S08-09 — Receipt component arithmetic review — FIXED

`RECEIPT_MISMATCH` kept the printed total, date, and category but withheld
confirmation solely because subtotal/tax/service/discount did not add up to
the total. This is a component quality signal under ADR-048, not new evidence
that the separately printed total is wrong. `receipt.go` now records
`arithmetic_ok=false` and confirms the known amount when no plausible duplicate
exists; one strong existing match links evidence without another review.
Missing facts and independent conflicts remain fail-closed. Historical open
`RECEIPT_MISMATCH` reviews remain resolvable for compatibility.

**Classification:** QUALITY_ONLY_REVIEW — removed for the complete-facts path.

---

# 4. Legacy reinterpretation disposition

| Legacy path | Disposition |
| --- | --- |
| Payslip generic `MANUAL_CORRECTION` branch | removed in SAVR-07; autonomous and reviewed paths both call `FinalizePayslip` |
| Receipt confidence-only category residual | removed post-SAVR-06 |
| Screenshot confidence-only category residual | removed post-SAVR-06 |
| Bank schema/transport failure as review | removed; machine/retry state |
| Provider `TRANSFER_CLASSIFICATION` for a known validation reason | narrowed post-SAVR-06; entity gaps use `FINANCIAL_EMAIL_RESOLUTION` |
| Bank source-event review projection | compatibility only; delivery fix tracked with UIR |
| Per-source merchant lookups | replaced by one `merchantmemory.Lookup` (SAVR-04) |

---

# 5. Exit

Every active source family is classified. S08-09 is fixed in the follow-up
receipt-materiality branch; UIR email-origin projection (S08-08) remains outside
SAVR scope. Do not equate this audit with a completed canary.


---

## 2026-09-28 closure ownership amendment

S08-08 is no longer an unowned "outside SAVR" follow-up. It is explicitly owned
by **UISC-01** in `docs/archive/RICHMOD_UIR_SAVR_CLOSURE_PRD.md`.

The defect is classified as UIR projection rather than SAVR semantic authority:
bank/financial-email reviews already have canonical household ownership.
UISC-01 removed their source-payload pre-gates in `main@1b70dd7` and the shared
projector now rejects stale and document-unbound cards (`main@a142537`).
Production observation of a naturally occurring email review remains UISC-03.

Closure criterion:

- BANK_EMAIL and FINANCIAL_EMAIL source provenance must not be used as a
  prerequisite for Telegram delivery;
- canonical household ownership drives recipient resolution;
- originating Telegram chat remains fallback only.
