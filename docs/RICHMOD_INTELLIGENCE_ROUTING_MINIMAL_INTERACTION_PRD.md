# RICHMOD — INTELLIGENCE ROUTING & MINIMAL HUMAN INTERACTION

## Product Requirements Document for Codex /goals

**Status:** APPROVED PRODUCT CONTRACT  
**Product decision:** BDR-001  
**Architecture:** ADR-045, amending ADR-038 / ADR-040 / ADR-041 / ADR-042  
**Repository:** raufimusaddiq/richmod  
**Audited implementation baseline:** main @ ab6ce956ad129eda240f1e88182e01fe2d654606  
**Date:** 2026-09-24  
**Backend:** Go  
**Frontend:** Next.js + React + JavaScript  
**Canonical store:** PostgreSQL  
**Inference boundary:** LiteRouter only  
**Bounded semantic inference:** Jev / System One  
**Generative inference:** configured cloud LLM providers  
**Timezone:** Asia/Jakarta  
**Currency:** IDR  

---

# 0. Document precedence and Codex execution rules

This PRD is the current authority for **intelligence routing, model-call ordering,
and the boundary between machine decisions and human interaction**.

It does not replace the original minimal-human-interaction product objective.
It refines how that objective is achieved.

When documents conflict, use this precedence for this initiative:

1. this PRD;
2. BDR-001;
3. ADR-045;
4. amended ADR-038 / ADR-040 / ADR-041 / ADR-042;
5. RICHMOD_MINIMAL_HUMAN_INTERACTION_VALID_DATA_PRD.md for unaffected product
   requirements;
6. older implementation plans/checklists.

The execution plan is:

- docs/plans/intelligence-routing-minimal-interaction-execution.md

The per-task drift gate is:

- docs/INTELLIGENCE_ROUTING_DRIFT_GUARD_CHECKLIST.md

## 0.1 Rules for /goals

When this initiative is executed through /goals, Codex MUST:

1. read this PRD, BDR-001, ADR-045, the original minimal-interaction PRD, and the
   execution plan before editing code;
2. fetch current main and verify the audited baseline above is still an ancestor;
3. re-audit current code for every task rather than blindly applying file paths
   from this document;
4. preserve unrelated behavior;
5. implement tasks in dependency order;
6. run the drift guard before marking each task complete;
7. add tests for **model-call ordering** as well as final canonical outcome;
8. never interpret "less human interaction" as permission to guess missing facts;
9. never interpret "Jev-first" as permission to run Jev after every generative
   call;
10. never interpret "LLM decisive" as "trust self-reported confidence alone."

A task is incomplete if its canonical result is correct but it introduces a
redundant intelligence pass or asks the user for a known fact.

---

# 1. Product thesis

Richmod should feel like autonomous bookkeeping, not AI-assisted data entry.

The user should provide evidence or intent once.

Richmod should:

~~~text
observe
extract
understand
classify
validate
reconcile
remember
record
~~~

without involving the user unless the remaining uncertainty is genuinely
irreducible or is a household policy choice.

The system should also avoid unnecessary model choreography.

The target is therefore:

~~~text
minimum necessary human interaction
+
minimum necessary intelligence work
+
valid auditable canonical financial data
~~~

---

# 2. Product optimization order

Richmod optimizes in this strict order.

## P0 — correctness and auditability

This is a hard constraint.

No amount of latency reduction or RHICE improvement may justify:

- fabricated facts;
- silent fallback timestamps presented as observed transaction dates;
- unsafe duplicate merge;
- guessed account ownership;
- guessed household policy;
- hidden mutation authority outside Go/PostgreSQL;
- model provider failure being treated as semantic approval.

## P1 — minimize required human interaction

Among valid paths, prefer the path that requires less human work.

One cheap machine decision is preferable to one avoidable user decision.

## P2 — minimize intelligence work

Among paths with equal correctness and equal human interaction, prefer fewer
model passes, lower latency, fewer provider dependencies, and lower cost.

This order is important.

Do not reduce model calls if doing so creates a preventable review.

Do not add a model call if it cannot reduce residual uncertainty or independently
protect correctness.

---

# 3. North-star metrics

## 3.1 Primary — RHICE

**Required Human Inputs per Canonical Financial Event**

~~~text
explicit human inputs required to reach valid canonical state
/
canonical financial events
~~~

Count actual human work:

- one button = 1 input;
- one dropdown = 1 input;
- one typed field = 1 input;
- amount + date entered in one form = 2 inputs;
- passive viewing = 0.

Targets:

~~~text
clear event                       RHICE = 0
one irreducible bounded choice    RHICE <= 1
N irreducible missing facts       RHICE <= N
known fact                        never increases RHICE
~~~

## 3.2 Correctness guardrail

**Post-Auto-Confirm Material Correction Rate**

Track material changes to:

- amount;
- transaction date/time;
- type;
- category;
- account relationship;
- duplicate/reconciliation state.

RHICE is not considered improved if correction rate materially regresses.

## 3.3 Intelligence efficiency metrics

Add/retain measurement for:

- Jev calls per canonical event;
- generative calls per canonical event;
- total intelligence passes per canonical event;
- p50/p95 inference latency by source;
- residual rescue count;
- residual rescue success rate;
- redundant semantic double-pass rate;
- time to canonical state.

### Redundant Semantic Double-Pass Rate

Definition:

~~~text
events where a semantic dimension was already policy-accepted,
then another model re-decided substantially the same dimension
without independent evidence purpose
/
AI-assisted canonical events
~~~

Target: approximately zero.

---

# 4. Core definitions

## 4.1 Bounded question

A question is bounded when Go already owns the complete output domain or
predicate.

Examples:

- route;
- category from active household categories;
- yes/no evidence support;
- review action;
- transfer purpose;
- salary response;
- merchant learning consent.

Preferred intelligence: Jev.

## 4.2 Generative requirement

A generative model is required when arbitrary output or open-ended capability is
needed.

Examples:

- vision;
- unknown merchant string;
- document field extraction;
- free-form description;
- complex multi-step conversational reasoning;
- arbitrary native-tool argument extraction.

Preferred intelligence: generative LLM.

## 4.3 Residual uncertainty

After deterministic validation, residual uncertainty is the smallest set of
facts still blocking canonical state.

Examples:

~~~text
known: amount, date, merchant
residual: category
~~~

or:

~~~text
known: amount, category
residual: transaction_at
~~~

Only residual uncertainty may trigger additional intelligence or required human
interaction.

## 4.4 Residual bounded rescue

A Jev call made **after** generative extraction is a rescue only when it receives
a bounded fact that remains unresolved.

Example:

~~~text
vision extracted receipt
category still uncertain
-> Jev chooses from household categories
~~~

This is desirable because it may eliminate a user review.

## 4.5 Independent evidence verification

A second bounded call may run after extraction when it answers a materially
different question.

Example:

~~~text
Extractor says amount = 53,000.
Verifier asks whether original bank evidence supports 53,000.
~~~

This is not redundant because extraction and evidence support are different
claims.

## 4.6 Redundant double-pass

Prohibited:

~~~text
LLM category = groceries, accepted by source policy
-> Jev asks category again
-> groceries
~~~

No new evidence was added and no residual uncertainty existed.

---

# 5. Semantic ownership

Every fact used in canonical mutation has one primary semantic owner for the
current event.

Allowed owner classes:

~~~text
DETERMINISTIC
JEV
GENERATIVE_EXTRACTION
USER
~~~

Examples:

| Fact | Typical owner |
| --- | --- |
| exact learned merchant mapping | deterministic |
| route | Jev |
| category from bounded household list | Jev or constrained generative extraction, depending on source |
| printed merchant text | generative extraction |
| arithmetic result | deterministic |
| duplicate safety | deterministic |
| canonical DB ID | deterministic |
| primary salary designation | user policy |
| residual allocation | user policy |

A fact may change owner only when the previous owner explicitly left it
unresolved.

Combined provenance is permitted only when models resolved different stages.

---

# 6. Canonical routing decision tree

The required architecture is:

~~~text
INPUT / EVIDENCE
      |
      v
DETERMINISTIC STATE / VALIDATION
      |
      +----------------------------------+
      |                                  |
question already bounded?          arbitrary capability needed?
      |                                  |
     YES                                YES
      |                                  |
      v                                  v
     JEV                           GENERATIVE LLM
      |                                  |
decisive?                         deterministic validation
  |     |                               |
 YES    NO                         complete + safe?
  |     |                          |            |
  v     |                         YES           NO
  GO    |                          |             |
        |                          v       residual bounded?
        |                          GO          |       |
        |                                     YES      NO
        |                                      |        |
        +-- generative only if                 v        v
            arbitrary output needed           JEV      USER /
                                               |        SAFE STOP
                                          decisive?
                                           |    |
                                          YES   NO
                                           |     |
                                           v     v
                                           GO   USER
~~~

Do not execute every box.

The tree chooses the minimum sufficient path.

---

# 7. Model-call budgets by source

These are target budgets for a clear event, not quotas that override safety.

## 7.1 Simple Telegram text

Example:

~~~text
jajan gorengan 5k
~~~

Target:

~~~text
Go generic harvest
-> 1 Jev bundle
-> Go canonical mutation
~~~

Budget:

~~~text
Jev:        1
Generative: 0
Human:      0
~~~

The Jev bundle should answer as many compatible bounded questions as possible in
one shared request.

Do not call LLM for prose if Go can produce the confirmation response.

## 7.2 Complex Telegram mutation

Example:

~~~text
kemarin habis sekitar lima puluh ribuan buat makan sama parkir, yang parkir 5k
~~~

A cheap route decision may establish that generative extraction is necessary.

Preferred:

~~~text
Jev route
-> 1 generative/native-tool pass
-> deterministic validation
-> Go
~~~

Budget on a clear generative result:

~~~text
Jev:        1 route pass
Generative: 1
Extra Jev:  0
Human:      0
~~~

If a named bounded residual remains, one residual Jev rescue is allowed.

Do NOT rerun the complete transaction semantic bundle merely because a
generative model participated.

## 7.3 Receipt image

Clear receipt:

~~~text
1 vision extraction
-> deterministic validation
-> canonical
~~~

Target:

~~~text
Jev:        0
Generative: 1
Human:      0
~~~

Receipt with unresolved category:

~~~text
1 vision
-> deterministic validation
-> 1 residual category Jev
-> canonical or category-only review
~~~

Receipt without date:

~~~text
1 vision
-> date genuinely absent
-> ask date only
~~~

Do not ask another model to invent the missing date.

## 7.4 Transaction screenshot

For one image:

~~~text
1 vision extraction
-> validate every row
-> clear rows continue
-> collect unresolved bounded rows
-> at most 1 batched Jev rescue
~~~

A 20-row screenshot with 17 clear categories and 3 uncertain categories should
not submit all 20 rows to Jev.

Desired:

~~~text
17 clear: no Jev
3 residual: one batched Jev
~~~

## 7.5 Bank email

Prefer deterministic parser/extracted evidence + Jev bounded judgments.

Evidence verification is allowed when it independently checks source support.

Merchant-less expense:

~~~text
valid amount/date/direction/channel
merchant = NULL
category unresolved
-> category Jev may still run
~~~

Merchant absence must not preempt category intelligence.

If category becomes decisive:

~~~text
CONFIRMED
merchant = NULL
RHICE = 0
~~~

## 7.6 Payslip

Generative/vision extraction may own printed facts.

Go validates arithmetic, dates, schema, and canonical relationships.

If the household already has primary salary policy and all event facts are
clear:

~~~text
no user review
no Jev approval replay
~~~

If first salary source requires designation:

~~~text
user policy choice
~~~

If date alone is missing:

~~~text
ask date only
~~~

Do not also ask salary classification when it is already determined by household
state.

## 7.7 Review actions

Exact bound replies may use deterministic binding + bounded Jev interpretation.

If an exact reply is already a canonical callback/button, no generative
interpretation is necessary.

Web and Telegram must consume the same ReviewDecision residual facts.

---

# 8. Source acceptance contract

Skipping redundant Jev does NOT mean confidence-only auto-confirmation.

Every source must define the deterministic conditions under which a generative
result is considered complete enough to continue.

Applicable checks include:

- strict schema/type decoding;
- allowed enum membership;
- positive valid IDR amount;
- transaction date provenance;
- arithmetic;
- duplicate/reconciliation search;
- source evidence conflict;
- household authorization;
- exact account/entity lookup;
- canonical category lookup;
- required-field completeness;
- human-policy exclusions;
- explicit ambiguous/missing field status.

A source MUST NOT use:

~~~text
confidence >= threshold
~~~

as its only canonical gate.

Confidence may contribute to policy only alongside the source acceptance
contract.

---

# 9. Missing evidence policy

Models resolve uncertainty.

They do not manufacture absent evidence.

If a required fact is absent:

~~~text
missing fact
-> ReviewDecision.missing_facts
-> user supplies only that fact
~~~

Examples:

### Receipt has no date

Correct:

~~~text
missing_facts = ["transaction_at"]
~~~

Incorrect:

~~~text
use upload time as observed transaction time
~~~

Incorrect:

~~~text
ask Jev to guess date
~~~

### Bank email has no merchant

If merchant is optional:

~~~text
merchant = NULL
~~~

Do not ask the user to invent one.

### Screenshot row category known, date absent

Correct:

~~~text
missing_facts = ["transaction_at"]
~~~

Do not create AMBIGUOUS_CATEGORY review merely because that was the old generic
review type.

---

# 10. Residual ReviewDecision contract

Before a user is asked anything, the producer MUST identify:

~~~text
known_facts
proposed_facts
missing_facts
conflicting_facts
bounded_choices
human_policy_choices
why_not_auto_confirm
~~~

The UI and Telegram may request only facts in the unresolved set.

## 10.1 Canonical confirm guard

A review MUST NOT reach CONFIRMED while a canonical-required missing fact remains
unresolved.

This must be validated server-side, not only in UI.

## 10.2 Compound uncertainty

If two facts are truly unresolved, represent both.

Example:

~~~text
missing_facts = ["transaction_at", "category"]
~~~

Do not hide one dimension inside a generic reason code.

## 10.3 Known fallback values

Internal fallback values used for indexing or processing must not be exposed as
known source facts.

Example:

~~~text
received_at fallback exists internally
transaction_at evidence missing
~~~

The ReviewDecision must say transaction_at is missing.

---

# 11. Failure behavior

## 11.1 Initial Jev route failure

For a mutation route, do not silently let the generative model answer the same
route question and mutate.

Allowed:

- safe retry under existing infrastructure retry policy;
- explicit safe stop;
- minimal clarification/review.

Read-only degradation may remain allowed where ADR-038 permits it.

## 11.2 Residual Jev failure

Keep valid generative extraction.

Do not discard known facts.

Create minimal review for only the residual bounded fact.

## 11.3 Generative failure

Use ADR-037 repair policy where applicable.

Otherwise preserve validated partial evidence and review only irreducible facts.

Jev cannot manufacture arbitrary strings the generative extractor failed to
observe.

---

# 12. Batching policy

Batch bounded residual work whenever questions share a state snapshot and doing
so does not widen the uncertainty set.

Good:

~~~text
3 uncertain screenshot rows
-> one Jev request with 3 row category questions
~~~

Bad:

~~~text
20 rows total
17 already clear
-> one Jev request with 20 questions
~~~

Also bad:

~~~text
3 uncertain rows
-> 3 separate Jev requests
~~~

unless request limits or correctness constraints require separation.

---

# 13. Product-specific behavior requirements

## 13.1 Telegram

MUST:

- preserve route-first implicit binding behavior;
- preserve exact binding precedence;
- avoid generative call when Jev fast path finishes;
- avoid second full Jev semantic replay after a clear required generative pass;
- use residual Jev only for named unresolved bounded dimensions;
- keep Go as mutation authority;
- ask only missing dimensions on failure.

MUST NOT:

- infer that high generative confidence alone means canonical support;
- let Jev provider failure silently reopen LLM mutation authority;
- let one open review swallow a new financial event.

## 13.2 Receipt

MUST:

- preserve duplicate safety;
- auto-confirm clear new receipt when source acceptance contract passes;
- accept a decisive constrained category without a mandatory Jev repeat;
- run category Jev only if category remains unresolved and candidates are
  bounded;
- review date only when date alone is missing;
- never canonicalize upload/received time as source-observed date.

## 13.3 Screenshot

MUST:

- treat row as certainty unit;
- skip Jev for rows whose bounded category is already source-policy accepted;
- batch only unresolved rows;
- preserve category conflict as unresolved even when a candidate ID exists;
- ask date only when category is already known and date is missing;
- keep income/transfer relationship human where evidence cannot establish it.

## 13.4 Bank email

MUST:

- preserve existing evidence-verification semantics;
- preserve deterministic learned merchant rule priority;
- run bounded category resolution for a merchant-less expense when enough other
  evidence exists;
- allow merchant NULL;
- never fabricate merchant;
- retain category-only review when category remains undecided.

## 13.5 Payslip

MUST:

- separate evidence extraction from household policy;
- not ask primary-salary classification if household state already resolves it;
- ask only date when only date is missing;
- keep first primary-salary designation human where product policy requires it.

## 13.6 Financial Provider Email

Existing partial-resolution contract remains.

Do not resubmit or recount server-known account/wealth IDs as fresh human input.

---

# 14. Telemetry contract

Telemetry must represent actual product behavior.

## 14.1 Human interaction

Persist enough information to count actual supplied controls/fields per
resolution.

Server-known merged values are NOT human inputs.

## 14.2 Intelligence phases

Each inference phase should record:

- source event;
- capability: JEV or GENERATIVE;
- purpose:
  - ROUTE
  - EXTRACTION
  - RESIDUAL_CATEGORY
  - EVIDENCE_SUPPORT
  - REVIEW_ACTION
  - OTHER_BOUNDED
- started/completed latency;
- outcome;
- policy/model version.

Do not store sensitive raw evidence in telemetry.

## 14.3 Double-pass detection

A double-pass detector should be derivable from:

~~~text
source_event
semantic_dimension
phase purpose
decision owner
~~~

If both LLM and Jev decided category, telemetry must show whether:

- LLM left category unresolved and Jev rescued it — valid;
- LLM category was accepted then Jev repeated it — redundant.

---

# 15. Acceptance scenarios

## A1 — simple Telegram

Input:

~~~text
jajan gorengan 5k
~~~

Expected:

- Go harvest succeeds;
- one Jev request can route and decide bounded transaction facts;
- no generative call;
- confirmed if decisive;
- RHICE 0.

## A2 — complex Telegram, clear generative result

Input requires arbitrary extraction not supported by generic harvester.

Expected:

- Jev route identifies generative need;
- one generative/native-tool pass;
- source acceptance checks pass;
- no second full transaction Jev replay;
- canonical confirm;
- RHICE 0.

## A3 — complex Telegram, category residual

Expected:

- route Jev;
- generative extraction;
- Go identifies only category residual;
- one category-only Jev rescue;
- if decisive, RHICE 0;
- if undecided, category-only review.

## A4 — Jev route unavailable

Expected for mutation:

- no hidden generative mutation fallback;
- safe explicit non-success/retry path.

## B1 — clear receipt

Expected:

~~~text
vision calls = 1
Jev calls = 0
human inputs = 0
~~~

provided deterministic receipt contract passes.

## B2 — receipt category uncertain

Expected:

~~~text
vision = 1
Jev residual = 1
human = 0 if Jev decisive
~~~

## B3 — receipt date absent

Expected:

~~~text
vision = 1
Jev date guess = 0
missing_facts = ["transaction_at"]
human inputs = 1
~~~

## C1 — screenshot 20 rows, all clear

Expected:

~~~text
vision = 1
Jev = 0
reviews = 0
~~~

## C2 — screenshot 20 rows, 3 categories uncertain

Expected:

~~~text
vision = 1
Jev = 1 batched request containing 3 unresolved rows only
~~~

17 clear rows never enter Jev.

## C3 — screenshot date missing, category known

Expected:

~~~text
review reason reflects missing date
missing_facts = ["transaction_at"]
~~~

## D1 — merchant-less Bank Email, decisive category

Expected:

~~~text
merchant = NULL
category Jev runs
CONFIRMED
RHICE = 0
~~~

## D2 — merchant-less Bank Email, category undecided

Expected:

~~~text
merchant remains NULL
review missing_facts = ["category"]
~~~

No merchant input.

## E1 — payslip missing date, primary salary already exists

Expected:

~~~text
missing_facts = ["transaction_at"]
~~~

No salary-classification control.

## E2 — first salary source and date missing

Expected:

~~~text
missing_facts includes transaction_at
human_policy_choices includes salary classification
~~~

No hidden machine inference for primary salary.

---

# 16. Anti-patterns explicitly prohibited

## 16.1 Consensus theater

~~~text
LLM says X
Jev says X
therefore safe
~~~

Agreement alone is not independent evidence.

## 16.2 Confidence authority

~~~text
LLM confidence > .95
therefore canonical
~~~

Prohibited without source acceptance contract.

## 16.3 Model guessing missing evidence

~~~text
date absent
-> ask another model
~~~

Prohibited.

## 16.4 Review as universal safety valve

~~~text
anything not deterministic
-> NEEDS_REVIEW
~~~

Prohibited.

## 16.5 Full-image Jev replay

~~~text
screenshot has 20 rows, 3 uncertain
-> Jev all 20
~~~

Prohibited.

## 16.6 Server-known values counted as user inputs

Prohibited in RHICE telemetry.

## 16.7 Provider failure as semantic negative/positive

Infrastructure failure must remain distinct from semantic decision.

---

# 17. Rollout strategy

Use independent rollout switches where current configuration pattern supports
them.

At minimum, independently reversible behavior should exist for:

- Bank Email category auto-confirm;
- receipt direct auto-confirm / residual category rescue;
- screenshot row auto-confirm / selective bounded rescue;
- Telegram post-generative direct acceptance if introduced.

Rollout order is defined in the execution plan.

Do not tune probability thresholds in the same change that rewires model
ordering unless a specific acceptance test proves the threshold change is
required.

---

# 18. Production evaluation

After rollout, evaluate both human and intelligence friction.

Required comparisons:

- RHICE before/after;
- human-touch rate;
- typed fields/event;
- accepted-without-edit rate;
- correction rate;
- Jev calls/event;
- generative calls/event;
- redundant double-pass rate;
- p50/p95 time-to-canonical-state.

A successful release should show:

~~~text
RHICE stable or lower
correction rate stable
redundant double-pass lower
latency stable or lower
~~~

A release that lowers model calls but raises RHICE is not a product win.

A release that lowers RHICE but materially raises correction rate is not a
product win.

---

# 19. Non-goals

This initiative does not:

- replace LiteRouter;
- replace Jev;
- remove generative models;
- move canonical authority out of Go;
- loosen household authorization;
- weaken duplicate/reconciliation safety;
- infer human policy choices;
- lower thresholds simply to improve metrics;
- introduce provider-specific document branches;
- add Python/Ollama or unrelated infrastructure;
- redesign unrelated product surfaces.

---

# 20. Definition of Done

## Product

- [ ] clear supported events require zero unnecessary human input;
- [ ] known facts are never re-entered;
- [ ] missing evidence is asked once and only for the missing dimension;
- [ ] human-policy decisions remain human.

## Intelligence routing

- [ ] bounded facts use Jev before generative when existing facts are sufficient;
- [ ] clear generative paths do not receive redundant Jev replay;
- [ ] residual bounded uncertainty gets Jev before user review where useful;
- [ ] independent evidence verification is explicitly distinguished from replay;
- [ ] clear screenshot rows never enter residual Jev batch;
- [ ] residual batches contain only unresolved questions.

## Safety

- [ ] confidence alone is not a canonical gate;
- [ ] missing date cannot become received/upload time silently;
- [ ] Jev provider failure cannot silently become LLM authority for the same
      bounded mutation decision;
- [ ] duplicate ambiguity remains fail-closed;
- [ ] merchant is never fabricated;
- [ ] account relationship and primary-salary policy are not guessed.

## Review

- [ ] ReviewDecision exactly represents residual uncertainty;
- [ ] backend refuses canonical confirmation if required residual facts are still
      missing;
- [ ] web and Telegram request the same missing facts.

## Telemetry

- [ ] RHICE counts actual user-supplied fields/choices;
- [ ] server-known values are excluded;
- [ ] Jev/generative phases are attributable by purpose;
- [ ] redundant double-pass rate is measurable;
- [ ] combined provenance identifies what Jev actually rescued.

## Tests

- [ ] model-call count/order is asserted for clear and residual paths;
- [ ] acceptance scenarios A1–E2 are covered at the appropriate unit/integration
      level;
- [ ] tests do not weaken the PRD by encoding a more review-heavy behavior;
- [ ] old tests that conflict with this PRD are deliberately updated with a
      comment referencing this contract.

---

# 21. Final product rule

For every event, ask these questions in order:

~~~text
1. Can deterministic state finish this safely?
   yes -> finish.

2. Is the unresolved question already bounded?
   yes -> Jev.

3. Does the task require arbitrary extraction/reasoning/vision?
   yes -> generative.

4. After deterministic validation, is anything still unresolved?
   no -> finish.

5. Is the residual uncertainty bounded and inferable?
   yes -> Jev rescue.

6. Is the evidence missing/conflicting or the decision fundamentally human?
   yes -> ask only that residual question.
~~~

At no point should Richmod ask:

~~~text
Can I make another model agree with the answer I already have?
~~~

The objective is not model consensus.

The objective is autonomous, correct, low-friction household bookkeeping.
