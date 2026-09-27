# Execution Plan — SAVR

**PRD:** `docs/RICHMOD_SEMANTIC_AUTHORITY_VALIDATION_RECONCILIATION_PRD.md`  
**Business decision:** `docs/bdr/BDR-004-semantic-authority-minimum-human.md`  
**Architecture:** ADR-047, ADR-048  
**Audit:** `docs/audits/SAVR-00-semantic-authority-boundaries.md`  
**Drift gate:** `docs/SAVR_DRIFT_GUARD_CHECKLIST.md`  
**Baseline:** `main@ffb29a15f13bef32fa8da0d840be75e3501fd928`

---

# Operating rules

1. Rebase/track latest main before each task.
2. Re-audit the exact boundary before editing.
3. One semantic owner per dimension.
4. Preserve hard canonical guards.
5. Do not add model calls to solve architecture uncertainty.
6. Prefer source/domain-specific typed changes over generic frameworks.
7. Add regression corpus coverage before deleting legacy semantics.
8. One bundled push per review cycle; wait for review of exact latest head.

---

# SAVR-00 — baseline semantic boundary audit

The initial audit is committed in:

`docs/audits/SAVR-00-semantic-authority-boundaries.md`

Before implementation, refresh:

- all generative/Jev → Go validation boundaries;
- all user confirmation/correction → mutation boundaries;
- household learned-knowledge readers;
- every validator that can create review;
- every domain event that can fall into generic transaction review;
- every ReviewDecision producer affected by validator output.

Output matrix:

```text
source/domain
semantic dimension
current owner
downstream reinterpretation
validator
consequence today
review residual today
target owner
target consequence
domain finalizer
```

Exit:

No source family in scope is missing from the matrix.

---

# SAVR-01 — observability + contract test foundation

Telemetry prerequisite: normalize all three dimension arrays at the worker
recorder boundary before PostgreSQL insertion, so omitted Jev residuals do not
become NULL. Contract helpers and product metrics remain part of SAVR-01.

The Operations product aggregate can derive known-fact re-asks from populated
ReviewDecision `knownFacts` / `missingFacts`; historical contracts without a
`missingFacts` array are excluded. The other three SAVR metrics require
validation-consequence / accepted-fact provenance and remain explicitly listed
as coverage gaps, not zero-valued rates.

## Objective

Make SAVR measurable before changing semantics.

Required:

- fix Jev intelligence phase telemetry nil `residual_dimensions`;
- add a regression for the recorder;
- add test helpers that can assert:
  - model/Jev call count by purpose;
  - known facts carried into ReviewDecision;
  - exact missing facts;
  - human-input attribution where available;
  - domain side effects.

Do not add a new telemetry table unless an existing metric cannot be derived.

Add/derive metrics for:

- Known Fact Re-ask Rate;
- Validator-Induced Human Review Rate;
- Residual Fidelity Rate;
- Semantic Re-decision Rate.

Initial implementation may expose these as deterministic test/operations
aggregates before any UI work. Do not build a new dashboard unless existing
Admin/Operations surfaces need a small extension.

Exit:

We can detect SAVR regressions without reading raw production SQL manually.

---

# SAVR-02 — validation consequence contract

## Objective

Stop boolean/error validators from collapsing unrelated meanings.

Introduce the smallest typed consequence vocabulary needed by real callers:

- HARD_CANONICAL_INVARIANT;
- REPRESENTATION_INVALID;
- INDEPENDENT_EVIDENCE_CONFLICT;
- BOUNDED_RESIDUAL;
- QUALITY_SIGNAL;
- HUMAN_POLICY;
- CANONICAL_AMBIGUITY.

Rules:

- no rules DSL;
- no generic engine;
- source validator may remain source-specific;
- consequence must carry the exact affected dimension(s);
- accepted facts are immutable unless consequence targets that dimension.

Start with tests over existing receipt/payslip/bank cases before broad adoption.

Exit:

At least the migrated validators can state why they blocked canonicalization
without inventing unrelated residuals.

---

# SAVR-03 — representation adequacy

## A. Screenshot missing amount

Change extraction/domain representation so a genuinely absent amount is
representable.

Required:

- no sentinel `"0"`;
- preserve merchant/date/direction/category;
- canonical transaction still requires positive amount;
- review missing facts = amount only when that is the only residual;
- repair cannot invent unseen amount.

## B. Payslip representation

Rework only the representation needed to prevent false semantic loss:

- period representation accepts supported real payroll ranges;
- payroll components can express the source faithfully enough that breakdown
  quality is not confused with net-pay validity;
- preserve net pay/pay date/employer/period independently.

Implementation slice: the printed range is validated against its month label,
then normalized to `YYYY-MM` for proposal/salary storage while the literal
period remains in extraction and `period_raw`. Missing gross pay is nullable,
and unfamiliar signed payroll lines are preserved as `other_components` rather
than used to invent a net-pay formula. Unproved arithmetic is a quality signal;
the payslip review/finalizer parity remains SAVR-07.

Do not model a full payroll accounting engine.

Exit:

Real missing/quality states are representable without fake values.

---

# SAVR-04 — shared household semantic knowledge

## Objective

Reuse exact learned knowledge before model/human escalation.

Extract a small shared resolver for the existing merchant/category rule behavior.

Consumers:

- receipt;
- screenshot;
- bank email;
- Telegram.

Rules:

- one exact normalized match;
- active household category;
- auto_apply + user-confirmed learning policy;
- user-authored precedence preserved;
- no fuzzy model matching in this resolver.

Do the same for another knowledge family only if the audit proves duplicate exact
logic and the shared contract is obvious.

Implementation: worker `merchantmemory.Lookup` uses one household-scoped exact
alias query for receipt, screenshot, bank email, and Telegram. It requires an
active household category and an explicitly confirmed auto-apply rule; a
conflicting screenshot category remains in review. Other knowledge families
remain source-specific until an actual duplicate contract is identified.

Exit:

A learned merchant/category fact is not forgotten because the next evidence
arrived through a different source family.

---

# SAVR-05 — user semantic authority

## A. Telegram pending batch

After explicit CONFIRM:

- validate structural/canonical invariants;
- do not re-run semantic approval for already presented/accepted fields;
- if a dimension was unresolved, the staged proposal must represent that before
  the confirm action is offered.

Regression:

```text
stage N valid items
user confirms
Jev semantic replay count = 0 for accepted dimensions
canonical writes = N
```

Implementation: explicit batch CONFIRM validates structural/canonical invariants
and active household categories, then writes the already-presented items once.
It does not call Jev again for accepted dimensions and does not fabricate bounded
judgment provenance for a human decision; the audit row carries the batch and
item identity. Staging now requires an active category for every expense, so a
missing one becomes review instead of an offer of confirmation.

## B. Single-record/correction paths

Audit dimension by dimension.

Remove only redundant re-decision.

Retain:

- true bounded residual Jev;
- independent evidence checks;
- canonical ID/invariant validation.

User correction should supersede machine semantic uncertainty when safe.

Exit:

Explicit user intent is not subordinated to a second semantic vote.

---

# SAVR-06 — exact residual fidelity

Migrate three confirmed collapse points.

## Receipt

- arithmetic quality cannot become `AMBIGUOUS_CATEGORY`;
- preserve known category/date/amount;
- review only a real blocker.

Implemented slice: the known-category/date arithmetic mismatch uses
`RECEIPT_MISMATCH` with typed `QUALITY_SIGNAL` / `receipt_arithmetic`, no
invented missing category, and a bounded confirm/ignore Telegram card. Other
receipt review outcomes remain for SAVR-08 source-family audit.

## Bank email

- persist negative/undecided evidence predicate outcomes;
- preserve transaction_at and other accepted facts;
- derive the exact residual/conflict from the failed predicate;
- a failed/undecided predicate blocks confirmation only when it is material to
  the canonical decision being attempted. For `SPENDING_ONLY` ingestion the
  payment mechanism (QR vs `DEBIT_CARD` vs `MERCHANT_PAYMENT`) is evidence
  metadata, not a required human fact, so an uncertain mechanism alone must not
  create a review or ask the household to resolve it;
- material predicates remain: transaction existence, amount, direction,
  SPEND-vs-TRANSFER/INTERNAL semantics, duplicate/reconciliation safety, and
  genuinely required category/account facts. A material independent-evidence
  conflict still fails closed.

Implemented slice: the bounded bundle rules on `transaction_observed`,
`amount_supported`, `direction_supported`, `semantic_grounded`, and
`material_ambiguity` — the old `channel_supported` claim is replaced by
`semantic_grounded`, which asks only whether the email supports the canonical
class (ordinary spend versus transfer/internal movement) and is explicitly
indifferent to the mechanism. A failed material predicate records
`claim_outcomes` on `bank_email_evidence_verification` and builds the review
with the exact affected/missing fact (`amount_idr`/`direction`/
`transaction_observed` as an `INDEPENDENT_EVIDENCE_CONFLICT`;
`transaction_ambiguity` as `CANONICAL_AMBIGUITY`; `transaction_semantics` as a
`BOUNDED_RESIDUAL`), preserving amount, date, direction, channel, and merchant.

## Financial-provider email

- classify structural/evidence/time/purpose/compatibility consequences;
- preserve `FINANCIAL_EMAIL_RESOLUTION + resolutionGaps`;
- use transfer classification only when transfer relationship/purpose is truly
  the residual.

Implemented slice: the bounded bundle records `claim_outcomes` per predicate, so
a cash movement whose typed evidence failed parks as the new
`FINANCIAL_EMAIL_FACTS` reason (migration `00071`) naming the exact unsupported
dimension (`observation_type`, `cash_movement`, `evidence_support`,
`transaction_ambiguity`) with only `IGNORE` allowed — no canonical write. A
transfer relationship/purpose residual and a date Go could not parse keep
`TRANSFER_CLASSIFICATION`. A case the bounded plane could not rule at all (an
unconfigured plane, a gateway outage, or a structurally incomplete observation)
does **not** park an IGNORE-only facts card from which the household could never
recover: it takes the `TRANSFER_CLASSIFICATION` recovery lane so the missing
amount or relationship can be supplied.

Exit:

ReviewDecision missing facts match the actual blockers for migrated cases.

---

# SAVR-07 — payslip domain continuity and finalizer parity

This is the most important domain migration.

Required architecture:

```text
payslip evidence
→ salary candidate
→ validation consequences
→ autonomous OR salary-domain review
→ SAME salary finalizer
```

Required behaviors:

- arithmetic mismatch does not convert payslip to generic MANUAL_CORRECTION;
- accepted amount/date/employer/period survive;
- first-primary-source choice remains human policy;
- existing primary + known pay date does not re-ask classification/date;
- reviewed primary salary produces the same salary_source / salary_event / cycle
  effects as the autonomous path;
- explicit human correction updates the candidate, then uses the finalizer.

Regression must compare canonical side effects, not just transaction status.

Exit:

No active payslip path loses salary identity merely because review was required.

---

# SAVR-08 — source-family parity and legacy isolation

Re-run the SAVR-00 matrix after SAVR-03..07.

For remaining active source boundaries:

- classify owner;
- classify validation consequence;
- prove known-fact preservation;
- prove exact residual;
- prove domain finalizer.

Do not rewrite healthy boundaries.

Mark remaining legacy reinterpretation as either:

- compatibility-only;
- intentionally independent evidence verification;
- scheduled for removal.

Exit:

No unexplained semantic re-decision remains in active source families.

---

# SAVR-09 — corpus, canary, and product metrics

Run the approved regression corpus.

Required:

- synthetic/redacted ATI payslip cases;
- screenshot missing amount;
- cross-source merchant memory;
- receipt arithmetic quality;
- bank single-predicate disagreement;
- provider-email exact residual;
- Telegram batch explicit confirmation;
- user correction;
- natural Indonesian date case after latest-main re-audit.

Measure:

- Known Fact Re-ask Rate;
- Validator-Induced Human Review Rate;
- Residual Fidelity Rate;
- Semantic Re-decision Rate;
- RHICE;
- material correction rate;
- calls/event.

Use existing kill switches where applicable.

Do not enable unified document interpretation merely to satisfy the canary.

Exit:

SAVR improves interaction/semantic efficiency without worsening correction rate.

---

# SAVR-10 — retire obsolete reinterpretation and freeze

Only after corpus/canary evidence:

- remove obsolete source-specific semantic re-parsers;
- remove duplicated merchant-learning lookups replaced by shared resolver;
- remove generic review fallbacks made unreachable by exact consequences;
- retain compatibility paths only where historical open state requires them.

Update architecture docs with final status.

Stop SAVR.

Next initiative: CEU.

---

# Task graph

```text
SAVR-00
   |
   v
SAVR-01
   |
   v
SAVR-02
   |
   +--------> SAVR-03 --------+
   |                          |
   +--------> SAVR-04 --------+
   |                          |
   +--------> SAVR-05 --------+--> SAVR-08 --> SAVR-09 --> SAVR-10
   |                          |
   +--------> SAVR-06 --------+
   |                          |
   +--------> SAVR-07 --------+
```

SAVR-03..07 may be separate PRs.

Do not run them as uncontrolled parallel edits over shared files. Respect the
repository's review FIFO.

---

# Task completion template

Every implementation task must report:

```text
Task:
Baseline main SHA:
Source/domain:
Semantic dimensions:
Owner before:
Owner after:
Canonical guards retained:
Validation consequence:
Known facts preserved:
Residual before:
Residual after:
Model calls before/after:
Human inputs before/after:
Domain finalizer:
Tests/corpus:
Drift guard:
Known follow-up:
```
