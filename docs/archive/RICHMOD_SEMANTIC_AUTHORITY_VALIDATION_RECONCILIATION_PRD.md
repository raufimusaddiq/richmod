# RICHMOD — SEMANTIC AUTHORITY & VALIDATION RECONCILIATION

## Product Requirements Document for Codex /goals

**Initiative:** SAVR — Semantic Authority & Validation Reconciliation  
**Status:** APPROVED PRODUCT DIRECTION / IMPLEMENTATION GATE  
**Repository:** raufimusaddiq/richmod  
**Audited baseline:** `main@ffb29a15f13bef32fa8da0d840be75e3501fd928`  
**Post-SAVR-06 reconciliation baseline:** `main@904ff1acf6bdd8954d49728d7cc2623b5eee8d45`  
**Reconciliation audit:** `docs/audits/SAVR-06-reconciliation-audit.md`  
**Date:** 2026-09-28  
**Depends on:** BDR-001, ADR-038, ADR-039, ADR-045, ADR-046  
**Product substrate:** UIR code product-closed; interactive production smoke remains deferred per UIR closure record

---

# 0. Document precedence and execution rules

For SAVR work, use this precedence:

1. this PRD;
2. BDR-004 — semantic authority with minimum human interaction;
3. ADR-047 — semantic fact ownership;
4. ADR-048 — validation consequence and domain continuity;
5. PRD #144 / BDR-001 / ADR-045 for intelligence routing;
6. ADR-039 for ReviewDecision;
7. ADR-046 for review projection/resolution;
8. older source-specific ADRs where not amended here.

Execution plan:

- `docs/plans/savr-execution.md`

Audit baseline:

- `docs/audits/SAVR-00-semantic-authority-boundaries.md`

Drift gate:

- `docs/archive/SAVR_DRIFT_GUARD_CHECKLIST.md`

Codex MUST re-check latest `main` before every implementation task. File paths in
the audit are evidence, not permission to blindly patch the same lines later.

---

# 1. Product problem

Richmod already has capable generative extraction, bounded Jev/System One
decisions, deterministic Go policy, canonical PostgreSQL state, and a
channel-independent review system.

The remaining failure mode is architectural:

```text
useful semantic fact is understood
        ↓
a downstream layer re-understands the same fact differently
        ↓
representation / threshold / regex / generic policy disagrees
        ↓
known information is erased or collapsed
        ↓
generic review or redundant model call
        ↓
human is asked for something Richmod already knew
```

Examples already present on the audited baseline include:

- a payslip with known salary facts falling into generic transaction correction
  when arithmetic validation blocks auto-confirm;
- screenshot amount being structurally required even when the image does not
  show one, allowing sentinel `"0"` that the deterministic validator later
  rejects;
- learned merchant category being reused by receipt/bank/Telegram paths but not
  by screenshot row category resolution;
- receipt arithmetic inconsistency being represented as `AMBIGUOUS_CATEGORY`
  even when category and date are already known;
- one failed bank evidence predicate collapsing into generic
  `transaction_semantics`;
- explicit pending-batch confirmation replaying semantic judgment for every item
  after the user has already confirmed the staged proposal.

This is not a reason to remove deterministic Go validation.

It is a reason to define what deterministic validation is allowed to decide.

---

# 2. North star

Richmod remains:

> **an autonomous household finance agent that understands evidence and
> conversation once, then keeps household financial state correct with the
> minimum necessary human interaction.**

SAVR makes that operational with one additional rule:

> **Each financial fact has one semantic owner. Downstream deterministic code may
> validate whether that fact is safe to canonicalize, but must not silently
> reinterpret, erase, or re-request it.**

Short form:

```text
LLM / Jev / user understands
        ↓
Go constrains and canonicalizes
        ↓
Go does not become a second, narrower semantic model
```

Or:

> **Deterministic Go owns canonical safety, not semantic re-interpretation.**

---

# 3. Product optimization order

SAVR inherits BDR-001:

1. correctness and auditability are hard constraints;
2. minimize required human interaction;
3. among equally correct paths, minimize intelligence passes, latency, cost, and
   implementation complexity.

SAVR MUST NOT improve RHICE by guessing.

SAVR MUST NOT improve correctness by asking the user to re-enter facts already
known.

## 3.1 Human-effort materiality

> **Only material uncertainty may increase RHICE.**

An uncertainty is material only when resolving it can change the attempted
canonical financial outcome, satisfy a hard canonical invariant, resolve a real
independent-evidence conflict, or obtain household policy that only a human can
supply.

An exact residual is not automatically a blocking residual.

```text
uncertain dimension
+ same canonical outcome whichever value wins
= provenance / quality metadata
= no human review
```

Examples:

- QR vs debit-card vs merchant-payment mechanism does not block a clear expense;
- generic model confidence does not by itself create a human task when all
  material facts have already satisfied the source acceptance contract;
- a redundant source hint does not become missing when the canonical household
  entity is already known;
- a system-derived timestamp allowed by source policy does not become missing
  merely because it was not printed by the source.

## 3.2 Semantic sufficiency ends semantic review

> **Once the selected minimum-sufficient intelligence path has resolved every
> material semantic dimension, semantic review is finished.**

The selected path may be:

```text
Go only
Jev only
LLM only
LLM -> Jev for one named bounded residual / independent evidence purpose
USER -> Go
```

SAVR never requires `LLM -> Jev` as a default pair. A complete source-acceptable
LLM result may proceed directly to canonical Go guards. Jev exists only when the
problem is bounded and still unresolved, or when it has a genuinely independent
evidence-verification purpose.

After semantic sufficiency, Go may enforce canonical safety. It may not require
another confidence vote, ontology vote, or model approval for the same outcome.

## 3.3 User effort is not an error-handling strategy

Machine inability is not automatically human uncertainty.

Provider outages, malformed model/schema output, parser limitations, and
representation defects must remain infrastructure/repair state unless there is a
specific material fact or household policy that the user can actually provide.

Creating a precise review item is still wrong when the user has no meaningful
decision to make.

---

# 4. Core invariants

## 4.1 One semantic owner per dimension

For one event and one semantic dimension, there is one current owner.

Owner classes:

- **DETERMINISTIC_KNOWLEDGE** — exact household rules, canonical IDs, explicit
  source configuration;
- **GENERATIVE_EXTRACTION** — arbitrary text/vision facts observed from evidence;
- **JEV** — bounded choice/predicate over a Go-owned possibility space;
- **USER** — explicit statement, confirmation, correction, or household policy.

Go may validate value shape, authorization, membership, invariants, or conflicts.

Go may not silently infer a different semantic value merely because an older
parser or heuristic exists.

Ownership may change only when:

- the owner explicitly left the dimension unresolved;
- new independent evidence conflicts with the current value;
- the user corrects it;
- a canonical invariant proves the value impossible.

## 4.2 Known facts survive downstream failure

> **A downstream validation failure must never erase an already accepted
> upstream fact.**

If amount/date/category/employer are accepted and payroll arithmetic is odd,
those accepted facts remain known.

If one bank evidence predicate fails, unrelated accepted facts remain known.

## 4.3 Residual fidelity

Human review and extra intelligence may address only the dimensions that still
block canonical state.

```text
known amount
known date
known category
arithmetic quality signal
        ↓
do not ask category
```

The ReviewDecision must describe the true blocker.

## 4.4 Domain continuity

> **A domain event does not lose its domain identity merely because it requires
> review.**

Examples:

- payslip remains a salary candidate;
- provider contribution remains a contribution/transfer candidate;
- receipt remains a receipt-backed expense candidate;
- bank transaction remains a bank-evidence transaction candidate.

Review is a pause in the domain lifecycle, not conversion to a generic object.

## 4.5 Canonical finalization parity

For the same domain event:

> **AUTO path canonical effects == HUMAN-RESOLVED path canonical effects, except
> fields explicitly changed by the human.**

For a primary salary payslip this includes, where applicable:

- transaction;
- salary source state;
- salary event;
- cycle progression / residual job;
- evidence links;
- audit/provenance.

## 4.6 Explicit user authority

When a user explicitly confirms or corrects a proposal they have been shown, that
semantic choice is authoritative unless a hard canonical invariant or new
independent conflict prevents it.

A later Jev call MUST NOT be used merely to ask whether the user "really meant"
the same thing.

Go still enforces:

- household authorization;
- valid active canonical IDs;
- supported currency;
- valid positive canonical amount where required;
- duplicate/concurrency constraints;
- domain compatibility;
- structural integrity.

## 4.7 System-derived policy facts are real canonical facts

A source/domain policy may deliberately derive a canonical fact when the source
does not provide a better one.

That fact must retain provenance, but provenance alone MUST NOT force review.

Bank-email example:

```text
printed transaction timestamp exists
-> transaction_at = printed timestamp
-> provenance = SOURCE_OBSERVED

printed transaction timestamp absent
-> transaction_at = source_event.received_at
-> provenance = EMAIL_RECEIVED_AT
-> no transaction-time review
```

This rule is source-policy-specific. It does not silently authorize the same
fallback for receipts, payslips, or screenshots unless their own source contract
explicitly says so.

## 4.8 Known canonical state outranks redundant source hints

When a canonical household entity/relationship has already been selected or
resolved, a missing redundant prose hint must not erase it or re-open the same
question.

A source hint can create a conflict when it materially disagrees with the known
canonical state. Its absence alone is not a conflict.

## 4.9 Candidate existence is not canonical ambiguity

A database/query candidate is only a search result.

It may increase RHICE only when the source/domain contract establishes that the
candidate is materially plausible for the same canonical event. A broad
same-amount/time-window query is not, by itself, proof that the household must
decide a duplicate.

Duplicate safety remains a hard canonical concern; this rule prevents weak
candidate generation from being confused with actual canonical ambiguity.

---

# 5. Accepted fact set — logical contract, not a new database

SAVR uses the term **Accepted Fact Set** for the set of semantic dimensions that
have already crossed their source-specific acceptance contract.

This is a logical architecture contract.

It does **not** imply a new universal `accepted_fact` table.

Prefer existing state:

- typed extraction structs;
- transaction proposals;
- ReviewDecision known/proposed/missing facts;
- source/domain rows;
- evidence/provenance metadata;
- judgment decisions;
- audit rows.

A new persistence structure requires proof that these existing structures cannot
preserve a required invariant.

## 5.1 Minimum fact metadata

Where a boundary needs explicit fact metadata, preserve only what is necessary:

```text
dimension
value
owner
provenance
acceptance state
```

Do not build a generic semantic graph or ontology engine.

Source/domain-specific typed structs are preferred when they are clearer.

---

# 6. Source acceptance vs canonical validation

These are different jobs.

## Source acceptance

Answers:

> Did this source/intelligence owner establish this semantic fact well enough for
> this source family?

Examples:

- vision extracted printed net pay;
- Jev chose one active category;
- exact merchant alias maps to one category;
- user explicitly supplied a date;
- bank evidence verifier confirmed the amount claim.

## Canonical validation

Answers:

> Can this accepted fact safely participate in canonical mutation?

Examples:

- category ID belongs to household and is active;
- amount is representable as positive IDR where a transaction requires it;
- timestamp parses;
- transfer relationship is compatible;
- no conflicting final write already won;
- duplicate/reconciliation guard is satisfied.

Canonical validation must not re-run source understanding.

---

# 7. Validation consequence taxonomy

Every validator failure that can affect product flow must map to one of these
consequences.

| Consequence | Meaning | Product action |
| --- | --- | --- |
| `HARD_CANONICAL_INVARIANT` | Value cannot exist safely in canonical state | reject/repair; never guess |
| `REPRESENTATION_INVALID` | Accepted/source value cannot be represented by current schema/type | targeted representation repair; preserve other facts |
| `INDEPENDENT_EVIDENCE_CONFLICT` | New independent evidence materially disagrees | bounded verification or exact conflict review |
| `BOUNDED_RESIDUAL` | One Go-owned bounded dimension remains unresolved | Jev once, then exact human choice if needed |
| `QUALITY_SIGNAL` | Consistency/confidence concern that does not itself erase authoritative facts | preserve facts; block only the capability truly affected |
| `HUMAN_POLICY` | Household intent is required | ask that policy choice only |
| `CANONICAL_AMBIGUITY` | Multiple safe canonical candidates remain | bounded human choice |

Do not replace one generic boolean with a generic framework.

A small typed result/error vocabulary is sufficient.

---

# 8. Representation adequacy

A model/tool contract must be able to represent reality.

## 8.1 Missing is not zero

For evidence fields that may genuinely be absent:

```text
missing / unknown != 0
missing / unknown != empty fabricated value
```

If screenshot amount is not visible, extraction must be able to represent it as
missing.

Canonical transaction mutation may still require a positive amount.

The gap then becomes:

```text
known: merchant/date/direction/...
missing: amount
```

not:

```text
amount = "0"
→ INVALID_AMOUNT
→ generic document failure
```

## 8.2 Payroll components

Payslip representation must distinguish enough semantics to avoid forcing all
visible payroll components into one unsigned arithmetic formula.

Do not infer payroll accounting that the document does not state.

The net-pay fact may remain accepted even when the detailed breakdown cannot be
fully reconciled.

## 8.3 Dates

An accepted date value should cross the canonical boundary as a typed date/time
with provenance.

Do not turn downstream Go into another natural-language date interpreter.

---

# 9. Household semantic knowledge

Learned household knowledge is deterministic semantic authority when its
preconditions are exact and current.

Examples:

- merchant alias → category;
- financial entity alias → account;
- known account relationship;
- established salary-source policy.

The same compatible household knowledge must be reusable across source families.

Current source-specific duplicate queries are migration evidence, not the target
architecture.

Target:

```text
household semantic knowledge
        ↓
one exact resolver contract
        ↓
receipt / screenshot / bank / Telegram / provider email
```

The resolver must:

- normalize once;
- require one unambiguous active result;
- remain household-scoped;
- preserve user-authored precedence;
- return canonical IDs/slugs, not model guesses.

---

# 10. Intelligence interaction rules

SAVR does not replace PRD #144.

The default is the **minimum sufficient intelligence path**, not repeated
consensus:

```text
deterministic knowledge sufficient -> Go only
bounded problem with facts present -> Jev only
arbitrary text/vision understanding -> LLM
LLM result source-acceptable and materially complete -> Go
LLM leaves named bounded material residual -> Jev on that residual only
explicit user authority -> Go
```

A model's self-reported confidence may be stored as quality/telemetry. It MUST
NOT silently become a third semantic authority or a human-review threshold after
material semantic sufficiency has already been reached.

## 10.1 Allowed post-generative Jev

Only when:

1. a named bounded dimension remains unresolved; or
2. Jev checks a materially different source-evidence claim.

## 10.2 Prohibited semantic replay

Examples:

- generative tool extracted a value and source policy accepted it, then Jev
  re-chooses the same value without new evidence;
- user explicitly confirms a staged batch, then Jev re-decides each already
  presented semantic dimension;
- user correction is sent through another semantic approval layer.

## 10.3 Provider failure

Provider failure is infrastructure state.

It must not:

- become semantic approval;
- erase accepted facts;
- be mislabeled as human ambiguity.

---

# 11. ReviewDecision contract under SAVR

UIR is frozen and remains the review substrate.

SAVR producers must feed it better residuals.

Before creating/updating a review:

```text
known facts
proposed facts
missing facts
conflicting facts
human policy choices
canonical ambiguity
why not auto
```

must reflect the actual event state.

Review type is presentation/routing vocabulary; it MUST NOT invent a missing
dimension merely because an older generic type exists.

If no existing review reason can truthfully represent the blocker, add the
smallest explicit reason/decision shape required. Do not overload an unrelated
reason such as `AMBIGUOUS_CATEGORY`.

---

# 12. Domain continuity contract

Each domain family that can enter review must define a finalizer.

A finalizer owns the canonical side effects of a successful domain event.

Surfaces and review paths call it; they do not recreate the side effects.

## Salary / payslip

Target:

```text
payslip evidence
  ↓
salary candidate
  ├─ known amount/date/employer/period/breakdown/provenance
  └─ validation consequences
  ↓
autonomous OR review
  ↓
same salary finalizer
  ↓
transaction
salary_source
salary_event
cycle effects
evidence/audit
```

Do not patch only the September period regex and call SAVR complete.

## Other domains

Apply the same rule where relevant:

- receipt-backed expense;
- bank transaction;
- provider contribution/withdrawal;
- wealth observation.

A generic transaction is allowed as canonical output, but the domain lifecycle
must remain available until domain-specific side effects are complete.

---

# 13. Source-family requirements

## 13.1 Payslip

Must:

- preserve extracted net pay, employer, period, actual receipt/pay date, and
  provenance independently of breakdown consistency;
- represent payroll breakdown without assuming every visible number belongs to
  one simplistic equation;
- classify arithmetic mismatch as a validation consequence, not generic loss of
  salary identity;
- keep an arithmetic-blocked payslip in the salary domain;
- resolve through the same salary finalizer as the autonomous path;
- preserve first-primary-source human policy where required;
- use actual salary receipt/pay date for salary-cycle anchoring when available.

Mandatory regression cases include the ATI August/September evidence and the
period string:

`September 2026 (01/09/26 - 30/09/26)`

A parser regex hotfix alone is insufficient.

## 13.2 Transaction screenshot

Must:

- allow genuinely missing amount/date/category fields to remain missing;
- never use sentinel zero to mean unknown;
- reuse exact learned merchant/category knowledge before Jev;
- run Jev only for actual bounded material residual rows;
- preserve other row facts if one field is missing;
- ask only the missing field(s);
- never convert a confidence-only auto-confirm miss into a fake category/date
  residual when those facts are already accepted;
- distinguish a materially plausible duplicate from a weak query candidate.

## 13.3 Receipt

Must:

- keep authoritative total/date/category facts even when breakdown arithmetic is
  inconsistent;
- distinguish arithmetic quality signal from category uncertainty;
- never create `AMBIGUOUS_CATEGORY` solely because arithmetic is inconsistent;
- never create `AMBIGUOUS_CATEGORY` solely because generic extraction
  confidence missed a threshold while category/date/amount are otherwise
  accepted;
- preserve duplicate safety without treating every weak candidate as a required
  human duplicate decision;
- reuse household merchant knowledge through the shared resolver.

## 13.4 Bank email

Must:

- persist the bounded predicate outcome that caused a review, including negative
  or undecided verdicts;
- preserve transaction_at and every other accepted extraction fact in
  ReviewDecision;
- map only **material** failed predicates to exact residual/conflict dimensions;
- never require a human to distinguish non-material payment mechanisms such as
  QR vs debit card when the canonical outcome is unchanged;
- preserve the explicit `EMAIL_RECEIVED_AT` timestamp fallback when no better
  transaction timestamp exists, without re-asking time;
- not collapse every unsupported predicate into generic
  `transaction_semantics`;
- keep independent evidence verification distinct from redundant semantic replay;
- keep schema/provider/transport failure separate from human uncertainty unless a
  material user-suppliable fact remains.

## 13.5 Financial-provider email

Must:

- replace generic `TRANSFER_CLASSIFICATION` fallback where the blocker is
  actually knowable and narrower;
- preserve the existing good `FINANCIAL_EMAIL_RESOLUTION + resolutionGaps`
  pattern for entity residuals;
- keep contribution/withdrawal/asset-purchase domain semantics through review;
- let already selected/resolved canonical accounts supersede redundant missing
  prose hints;
- avoid bundled evidence predicates whose non-material subpart can block an
  otherwise complete canonical movement;
- ask only unresolved **material** entities/relationship/policy;
- never create human work merely to acknowledge a machine/provider inability
  when the household has no fact or policy to add.

## 13.6 Telegram

Must:

- distinguish user-authored facts from model proposals;
- preserve explicit corrections/confirmations;
- stop semantic replay after explicit confirmation when only canonical checks
  remain;
- retain Jev when there is a true bounded residual or independent evidence
  purpose;
- re-audit the single-record path dimension by dimension rather than deleting
  Jev wholesale.

Batch confirmation is a mandatory regression case.

---

# 14. Telemetry and product metrics

SAVR needs observability that measures semantic waste, not only model calls.

## 14.1 Metrics

### Known Fact Re-ask Rate

```text
reviews/human prompts that request a dimension already accepted before review
/
reviews/human prompts
```

Target: **0%**.

### Validator-Induced Human Review Rate

Reviews caused solely by representation/validation mismatch where no genuine
semantic or policy dimension was unresolved.

Target: trend toward zero; every remaining case must be explainable.

### Residual Fidelity Rate

```text
review missing_facts exactly equal the actual unresolved dimensions
/
reviews sampled/measured
```

Target: **100%** for migrated source families.

### Semantic Re-decision Rate

```text
accepted semantic dimensions re-evaluated without new independent evidence
/
AI-assisted events
```

Target: approximately **0**.

Retain:

- RHICE;
- post-auto-confirm material correction rate;
- generative/Jev call counts;
- time to canonical state.

## 14.2 Intelligence phase telemetry correctness

Telemetry failure must not silently make SAVR look healthier than reality.

On the audited baseline, the Jev instrumentation maps `systemone.Metric` into
`gateway.CallMetric` without `ResidualDimensions`, while the insert explicitly
writes `residual_dimensions` into a NOT NULL column.

SAVR must fix/cover this before relying on the phase telemetry for rollout
measurement.

Prefer normalizing nil to an empty array at the recorder boundary rather than a
schema redesign unless current storage proves insufficient.

---

# 15. Regression corpus

The implementation must maintain a deterministic fixture/corpus covering at
least:

1. ATI August/September payslip;
2. period `September 2026 (01/09/26 - 30/09/26)`;
3. salary amount/date/employer known + payroll arithmetic mismatch;
4. screenshot with amount genuinely not visible;
5. learned merchant alias later seen through screenshot;
6. receipt with known total/date/category + inconsistent arithmetic;
7. bank verification where exactly one predicate is negative/undecided;
8. bank case with known transaction_at preserved into review;
9. financial-provider contribution/withdrawal with exact entity residual;
10. Telegram multi-item staged batch + explicit confirmation;
11. user correction of a semantic fact;
12. natural Indonesian date input — first re-audit latest behavior and then pin a
    regression only if a redundant re-parse/redecision still exists.

Do not put real production financial documents into public fixtures if the repo
does not already have safe redacted equivalents.

Synthetic fixtures must preserve the relevant structure, not personal content.

---

# 16. YAGNI / non-goals

SAVR MUST NOT become a platform rewrite.

Do not build:

- a universal workflow engine;
- a generic rules DSL;
- a semantic graph;
- a new event bus;
- a new vector store;
- a new AI model layer;
- model consensus voting;
- a universal `accepted_fact` database table by default;
- a generic validation microservice;
- a second review subsystem;
- a full document pipeline replacement.

Do not enable disabled unified document interpretation merely because SAVR
exists.

Do not rewrite all source families in one PR.

Do not remove old validators until regression evidence proves the replacement
path.

---

# 17. Definition of Done

SAVR is product-complete when:

1. every migrated semantic dimension has an explicit owner;
2. downstream validation cannot silently replace an accepted semantic value;
3. known facts survive validation failures;
4. representation can encode genuine missingness for migrated sources;
5. household learned knowledge is reused consistently across compatible sources;
6. explicit user confirmation/correction is not semantically replayed without a
   distinct reason;
7. reviews ask only actual **material** residual dimensions;
8. exact-but-non-material uncertainty cannot increase RHICE;
9. semantic sufficiency ends semantic review: no generic confidence/model vote
   blocks the same already-resolved canonical outcome;
10. source-policy-derived canonical facts retain provenance without becoming
    human missingness;
11. machine/provider/schema failure is not projected as human uncertainty unless
    the user has a material decision to make;
12. bank negative/undecided verification provenance is persisted and review
   residual is exact and material;
13. receipt arithmetic mismatch or confidence-only gating cannot masquerade as
    category ambiguity;
14. payslip review retains salary identity;
15. payslip autonomous and human-resolved paths use the same salary finalizer and
    produce equivalent domain effects;
16. SAVR metrics are observable without relying on broken phase telemetry;
17. corpus regressions pass;
18. PRD #144 minimum-sufficient call-ordering and UIR contracts remain green;
19. obsolete duplicate reinterpretation logic is removed only after migrated
    paths prove stable.

---

# 18. Stop condition

After SAVR closes, stop semantic cleanup and move to CEU.

Do not expand SAVR into conversational evidence unification.

CEU owns combining attachment, caption, reply, and ordinary conversational
evidence into one turn context.

SAVR owns what happens **after a semantic fact exists**: who owns it, how it is
validated, whether it survives, and exactly what remains unresolved.
