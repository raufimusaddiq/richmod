# Execution Plan — SAVR

**PRD:** `docs/RICHMOD_SEMANTIC_AUTHORITY_VALIDATION_RECONCILIATION_PRD.md`  
**Business decision:** `docs/bdr/BDR-004-semantic-authority-minimum-human.md`  
**Architecture:** ADR-047, ADR-048  
**Audit:** `docs/audits/SAVR-00-semantic-authority-boundaries.md`  
**Drift gate:** `docs/SAVR_DRIFT_GUARD_CHECKLIST.md`  
**Initial baseline:** `main@ffb29a15f13bef32fa8da0d840be75e3501fd928`  
**Post-SAVR-06 reconciliation baseline:** `main@904ff1acf6bdd8954d49728d7cc2623b5eee8d45`  
**Reconciliation audit:** `docs/audits/SAVR-06-reconciliation-audit.md`

---

# Sprint status after SAVR-06 merge

Status is based on exit criteria, not PR names.

| Sprint | Status | Reconciliation note |
| --- | --- | --- |
| SAVR-00 | ✅ baseline complete | preserve original audit as historical evidence |
| SAVR-01 | 🟡 partial | telemetry arrays + known-fact re-ask metric delivered; remaining metric coverage is still explicitly incomplete |
| SAVR-02 | 🟡 partial | consequence vocabulary and migrated receipt consequence delivered; not every active validator is consequence-aware yet |
| SAVR-03 | ✅ scoped delivery | screenshot missing amount + payslip representation delivered |
| SAVR-04 | ✅ delivered | one shared exact household merchant/category memory path |
| SAVR-05 | 🟡 materially aligned | batch user authority delivered; current single-record path accepts complete generative results directly and spends Jev only on named residuals; remaining call sites are re-audited in SAVR-08 |
| SAVR-06 | 🟡 merged, reconciliation required | PR #202 fixed bank payment-mechanism materiality and provider residual fidelity, but post-merge audit still finds cross-source hard-gate debt listed below |
| SAVR-07 | ✅ delivered (commit `c70864f`) | payslip domain continuity; both paths share `FinalizePayslip` |
| SAVR-08 | 🟡 audited, fixes delivered | semantic/source-family fixes delivered; S08-08 email-origin projection is owned by UISC-01 in the final combined closure |
| SAVR-09 | ✅ product-complete (UISC-04, 2026-10-03) | UISC-02 completes three observability gaps; owner-household observation accepted under BDR-005; unobserved families stay `PRODUCTION_UNOBSERVED` |
| SAVR-10 | ✅ full freeze (UISC-04, 2026-10-03) | corpus-proven removals done; frozen except production defects against the approved contract |

# Post-SAVR-06 reconciliation gate

This is a **gate**, not a new product initiative.

Before SAVR-07 implementation starts, the next implementation work must reconcile
the confirmed post-06 drift against the north star:

1. receipt: generic extraction confidence must not be the sole reason a
   fully-known amount/date/category expense falls into a fake category review;
2. screenshot: same rule — a confidence-only miss cannot manufacture
   `AMBIGUOUS_CATEGORY` when the material facts are already accepted;
3. provider email: an already selected/resolved canonical account must supersede
   a redundant missing `FundingAccountHint`;
4. provider email: bundled `evidence_sufficient` must not let a non-material
   hint subpart block an otherwise complete movement; split/derive material
   residuals only as needed, without adding another AI layer;
5. machine/schema/provider inability must remain retry/repair/infrastructure state
   unless there is a specific material fact or policy the household can supply.
   Malformed document output or failed field repair (receipt, screenshot, or
   terminal classification) is recorded as `FAILED`/unvalidated extraction;
   it cannot open a `DOCUMENT_*` review merely to re-run a machine step;
6. duplicate matching must distinguish a materially plausible duplicate from a
   weak query candidate. Preserve duplicate safety; do not use candidate
   existence alone as proof that human work is required. For receipt/screenshot
   same-amount candidates, an exact merchant within 72 hours or a transaction
   within one hour remains plausible; a different merchant 12–72 hours apart
   is only a query hit.

Adjacent UIR defect, tracked separately from SAVR semantics:

- email-origin canonical reviews must not be Inbox-only merely because the source
  was not Telegram. Projection policy belongs to UIR and should use the
  household's eligible Telegram recipient where configured.

Healthy behavior that this gate MUST NOT regress:

- bank `EMAIL_RECEIVED_AT` is an intentional canonical timestamp fallback when
  the email has no better transaction time;
- QR/debit-card/payment-mechanism uncertainty is non-material for an otherwise
  clear `SPENDING_ONLY` expense;
- complete Telegram generative transaction extraction may proceed without a Jev
  replay;
- exact merchant memory avoids model/human escalation;
- explicit user batch confirmation is not sent through a second semantic vote.

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
`amount_supported`, `direction_supported`, and `semantic_grounded` — the old
`channel_supported` claim is replaced by
`semantic_grounded`, which asks only whether the email supports the canonical
class (ordinary spend versus transfer/internal movement) and is explicitly
indifferent to the mechanism. `material_ambiguity` was removed on 2026-09-29:
certifying the absence of ambiguity is open-ended, so an undecided answer parked
complete emails for review. A failed material predicate records `claim_outcomes`
on `bank_email_evidence_verification` and builds the review with the exact
affected/missing fact (`amount_idr`/`direction`/`transaction_observed` as an
`INDEPENDENT_EVIDENCE_CONFLICT`; `transaction_semantics` as a
`BOUNDED_RESIDUAL`), preserving amount, date, direction, channel, and merchant.

Implementation slice: store all bounded predicate outcomes (YES/NO/UNDECIDED)
before routing unsupported evidence to review. The bank review carries affected
facts and exact missing/conflict dimensions; unrelated accepted facts, including
the transaction timestamp, remain known. A negative predicate does not invent
an alternative source value. Provider-email residual parity remains separate.

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
failed `evidence_support` claim keeps extracted values as proposed, not known;
the Inbox labels proposed values independently of the decision source, so an
unsupported value cannot appear as recorded data. Bank evidence conflicts
remain higher priority than ambiguity, and ambiguity higher priority than an
undecided material fact;
ignoring the review settles both the provider-email and Telegram callback
source events. A
transfer relationship/purpose residual and a date Go could not parse keep
`TRANSFER_CLASSIFICATION`. A genuinely missing amount, time, or account may
take a bounded human recovery lane. A provider/schema failure alone stays
retry/infrastructure state, never `TRANSFER_CLASSIFICATION` review. An
unconfigured bounded plane alone does not make a complete LLM extraction a
human question. The provider evidence predicate checks material value and time,
not a redundant prose account hint after canonical account resolution. A
resolvable contradictory account hint remains a material evidence conflict:
`FINANCIAL_EMAIL_RESOLUTION` presents both values; the user's scoped account
decision closes that conflict on replay without asking again.

Exit:

ReviewDecision missing facts match the actual blockers for migrated cases.

---

# SAVR-07 — payslip domain continuity and finalizer parity

**Start condition:** the Post-SAVR-06 reconciliation gate above is accepted and
its implementation corrections are merged. Do not carry known confidence-only,
hint-only, or machine-failure review patterns into the salary finalizer.

This is the most important domain migration.

Implementation slice (2026-09-28): an accepted payslip with a known pay date
and an existing primary salary uses the same Go salary finalizer as a resolved
payslip review. Generic extraction confidence and an unreconciled payroll
breakdown are quality signals, not human work. Missing date and first-source
policy stay exact payslip reviews; accepted amount, employer, period, and date
survive the review. The salary finalizer writes one income transaction, payslip
evidence, salary source/event, cycle job for a primary salary, and audit in one
database transaction; duplicate period/employer evidence enriches the existing
salary transaction only if amount and pay date also agree. A material conflict
fails closed. Generic MANUAL_CORRECTION is not used for this lane.

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
- generic payslip confidence is not, by itself, a human-review reason once the
  material salary facts have satisfied their source contract;
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

**Audit:** `docs/audits/SAVR-08-source-family-parity.md` (this branch). It
recorded an unrepairable payslip extraction opening a human review; the payslip
path now uses the shared failed-extraction writer, like receipt and screenshot.
The S08-09 receipt arithmetic quality review is removed in the follow-up
branch; the adjacent UIR projection defect (S08-08) remains open.

Re-run the SAVR-00 matrix after SAVR-03..07, using **materiality** and
**minimum-sufficient intelligence** as first-class columns.

For every boundary, additionally record:

```text
attempted canonical outcome
material semantic dimensions
non-material metadata/quality dimensions
minimum sufficient intelligence path
why each additional AI call exists
why each human input exists
system-derived policy facts
```

A boundary is not parity-complete merely because its review residual is exact.
The residual must also be material.

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

**Result:** corpus measured and production deployed. BDR-005 supersedes the
disposable-household assumption: the real owner household is the production
observation cohort. UISC-02 must make Validator-Induced Human Review Rate,
Residual Contract Fidelity, and Semantic Re-decision Rate prospectively
measurable without fabricating historical zeroes.

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

**Result:** corpus-proven removals are documented in
`docs/audits/SAVR-10-legacy-retirement.md`. The freeze is **full** as of UISC-04
(2026-10-03): the UIR-SAVR Closure Sprint closed S08-08, completed observability,
and the owner-household production observation was accepted.

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
   +--------> SAVR-05 --------+
   |                          |
   +--------> SAVR-06 --------+
                              |
                              v
                  POST-SAVR-06 RECONCILIATION
                              |
                              v
                           SAVR-07
                              |
                              v
                           SAVR-08 --> SAVR-09 --> SAVR-10
                                                   |
                                                   v
                                      UIR-SAVR CLOSURE
                                                   |
                                                   v
                                                  CEU
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
Attempted canonical outcome:
Semantic dimensions:
Material semantic dimensions:
Non-material dimensions:
Minimum sufficient intelligence path:
Owner before:
Owner after:
Canonical guards retained:
Validation consequence:
Known facts preserved:
Residual before:
Residual after:
Model calls before/after:
Why every additional model call exists:
Human inputs before/after:
Why every human input exists:
System-derived policy facts:
Could the same canonical outcome be reached without this review:
Domain finalizer:
Tests/corpus:
Drift guard:
Known follow-up:
```


## 2026-09-28 final closure routing

The UIR-SAVR Closure Sprint defined by
`docs/RICHMOD_UIR_SAVR_CLOSURE_PRD.md` is the only remaining pre-CEU gate.

It supersedes the disposable-production-household assumption but does not weaken
the SAVR corpus or canonical correctness requirements. Real owner-household
usage is the production observation cohort; synthetic edge cases stay in
disposable test infrastructure.
