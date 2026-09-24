# Execution Plan — Intelligence Routing & Minimal Human Interaction

## Status

Ready for /goals execution.

## Source contracts

Read before implementation:

1. docs/RICHMOD_INTELLIGENCE_ROUTING_MINIMAL_INTERACTION_PRD.md
2. docs/bdr/BDR-001-minimum-human-interaction-single-pass-intelligence.md
3. docs/adr/ADR-045-single-intelligence-pass-routing.md
4. docs/RICHMOD_MINIMAL_HUMAN_INTERACTION_VALID_DATA_PRD.md
5. docs/INTELLIGENCE_ROUTING_DRIFT_GUARD_CHECKLIST.md

Audited baseline:

~~~text
main = ab6ce956ad129eda240f1e88182e01fe2d654606
~~~

Codex MUST fetch current main before starting. If the baseline is no longer an
ancestor, re-audit each task against current code and preserve the contract
rather than copying stale file edits.

---

# 1. Goal

Close the remaining PRD drift while preserving the original product objective:

~~~text
minimum necessary human interaction
subject to valid financial data
~~~

and add the newly explicit efficiency rule:

~~~text
minimum necessary intelligence work
among equally correct, equally low-human-interaction paths
~~~

The implementation must not replace review friction with model-chain friction.

---

# 2. Delivery shape

Recommended implementation grouping:

## Wave A — correctness foundation

- IR-01 residual ReviewDecision correctness
- IR-02 canonical confirmation guards
- IR-03 RHICE measurement correctness

These tasks establish trustworthy residual state and metrics before changing
model ordering.

## Wave B — intelligence routing

- IR-04 Telegram single-pass routing
- IR-05 Receipt residual bounded rescue
- IR-06 Screenshot selective bounded batch
- IR-07 Bank Email merchant-less category resolution
- IR-08 Payslip residual-policy separation

## Wave C — observability and hardening

- IR-09 intelligence-pass telemetry
- IR-10 regression/canary/rollout hardening

Do not combine unrelated cleanup into these waves.

Each task may be a separate PR or a tightly related bundle. If bundled, all
individual task acceptance gates remain mandatory.

---

# 3. Global implementation constraints

Every task MUST preserve:

- Go/PostgreSQL canonical authority;
- LiteRouter-only inference boundary;
- household authorization;
- duplicate/reconciliation fail-closed behavior;
- exact-vs-implicit Telegram binding precedence;
- current source evidence/audit linkage;
- rollback switches already present;
- existing user-policy choices.

Every task MUST NOT:

- lower Jev/model thresholds merely to reduce review rate;
- add generic provider-specific branches;
- introduce mandatory LLM -> Jev replay;
- replace a Jev-owned failure with generative mutation authority;
- invent missing evidence;
- count server-known values as human input;
- mark combined provenance unless both model phases actually contributed.

---

# 4. IR-00 — Baseline re-audit

## Purpose

Prevent stale plan execution.

## Required actions

Before code edits:

- fetch latest main;
- inspect all current implementation paths named below;
- list any already-fixed behavior;
- identify any new conflict with this PRD;
- update task notes only if code moved, not product intent.

Minimum paths to inspect:

- apps/worker/internal/telegram/
- apps/worker/internal/document/receipt.go
- apps/worker/internal/document/screenshot.go
- apps/worker/internal/document/screenshot_choice.go
- apps/worker/internal/document/payslip.go
- apps/worker/internal/bankemail/
- apps/worker/internal/reviewdec/
- apps/api/internal/review/
- apps/api/internal/operations/product.go
- apps/web/app/components/ReviewCards.js
- relevant migrations/tests/ADRs

## Exit gate

Produce a short implementation note mapping IR-01..IR-10 to current files and
confirm no product requirement has been silently dropped because code moved.

---

# 5. IR-01 — Residual ReviewDecision correctness

## Current audited drift

At the audited baseline:

- receipt without source transaction date can carry an internal received/upload
  fallback while review semantics still use category-oriented reason;
- screenshot row with category known but date missing can still be represented as
  AMBIGUOUS_CATEGORY;
- compound category conflict + missing date is not guaranteed to preserve both
  residual dimensions;
- known fallback timestamps risk appearing as if they were observed facts.

## Product requirement

ReviewDecision must represent the real residual uncertainty, not the historical
review type.

Required reason/contract behavior:

~~~text
category only unresolved
-> missing_facts = ["category"]

transaction date only unresolved
-> missing_facts = ["transaction_at"]

category + date unresolved
-> missing_facts = ["category", "transaction_at"]

duplicate ambiguity
-> conflicting/bounded duplicate candidates

transfer relationship unknown
-> policy/bounded relationship choice
~~~

Internal fallback timestamps are provenance, not known source facts.

## Implementation notes

Codex may add or refine reason codes. Suggested semantics:

- MISSING_TRANSACTION_DATE
- TRANSACTION_FACTS_MISSING
- AMBIGUOUS_CATEGORY
- POSSIBLE_DUPLICATE

Do not create a generic full-form reason if exact missing dimensions are known.

## Required tests

Receipt:

- clear category + missing date -> date-only decision;
- category missing + date missing -> both facts;
- received_at fallback absent from known_facts.transaction_at.

Screenshot:

- clear category + missing date -> date-only decision;
- category conflict + missing date -> both residual facts;
- duplicate path still outranks ordinary missing/category review where duplicate
  resolution is the real blocker.

## Exit gate

Every new receipt/screenshot review can answer:

~~~text
What exact fact still blocks canonical confirmation?
~~~

with no extra field.

---

# 6. IR-02 — Canonical confirmation guard

## Problem

UI correctness is not enough. A legacy client, Telegram path, or old review card
must not confirm a transaction while a required residual fact remains missing.

## Requirement

Before canonical CONFIRMED transition, backend validates the stored
ReviewDecision residual contract.

If required missing_facts includes transaction_at:

- explicit user/source value must be supplied;
- internal received/upload fallback does not satisfy it.

If missing_facts includes category for an expense:

- a valid household category must be supplied/resolved.

If both are missing:

- both must be resolved before confirm.

## Surfaces

Apply equivalent guard semantics to:

- Web Review API;
- Telegram exact/native review resolution;
- any legacy bound review resolver still capable of confirming the same
  transaction.

## Required tests

- Web confirm missing date -> 4xx / unresolved;
- Telegram confirm missing date -> remains review, requests date;
- category-only review accepts category without asking date;
- compound review refuses partial resolution;
- once all residual facts are supplied, one canonical confirm occurs;
- no duplicate proposal/transaction is created.

## Exit gate

No client path can canonicalize an internal fallback as an observed missing fact.

---

# 7. IR-03 — RHICE measurement correctness

## Current audited drift

At baseline, product aggregate logic can approximate one resolved review as one
human input and typed-field actions as one typed field, even when a form contains
multiple controls. Server-known IDs merged into resolution payload can also look
like new user input.

## Product requirement

RHICE counts actual human work.

Examples:

~~~text
Accept button                  1 input
Category dropdown              1 input
Date field                     1 input
Amount + date form             2 inputs
Known account merged by server 0 inputs
~~~

## Required implementation

Review-turn telemetry must distinguish:

- values the user supplied this turn;
- values loaded/merged from known server state.

Do not infer typed-field count solely from action name.

Preserve privacy: field names/action classes are enough; raw financial values do
not need to enter product telemetry.

## Required tests

- amount + date = 1 turn that counts 2 typed inputs;
- one proposal acceptance = 1 input, 0 typed fields;
- one unresolved financial-email entity = 1 input even when server merges the
  other known entity;
- system resolution = 0;
- IGNORE does not become a canonical-event input;
- out-of-window resolution does not enter numerator.

## Exit gate

Dashboard RHICE cannot improve merely because several fields were submitted in
one HTTP request.

---

# 8. IR-04 — Telegram single-pass routing

## Current audited shape

The first Jev route/bounded fast path already exists and can completely handle
simple harvestable transactions.

Valid non-fast routes fall through to the generative/native-tool agent.

The drift risk is a later complete transaction-semantic Jev call that re-decides
facts after the generative tool already produced a clear constrained result.

## Required product flow

### Simple harvestable transaction

~~~text
Go harvest
-> one Jev route + transaction bundle
-> Go
~~~

No generative call.

### Complex transaction requiring generative extraction

~~~text
Jev route
-> generative/native-tool
-> deterministic acceptance contract
-> Go
~~~

No full second Jev replay if the result is complete and safe.

### Residual bounded uncertainty

~~~text
Jev route
-> generative
-> Go identifies named residual fact
-> Jev rescue for that fact only
-> Go/review
~~~

## Acceptance contract

Codex MUST define explicit deterministic checks for direct post-generative
acceptance.

Do not use only:

~~~text
confidence >= threshold
~~~

The checks should include applicable:

- constrained type;
- positive IDR amount;
- date provenance/resolution;
- allowed category;
- no explicit ambiguous flag;
- route permits mutation;
- household/category authorization;
- no duplicate/edit conflict;
- all canonical-required facts present.

If a fact cannot be safely supported under this contract, that fact becomes
residual and may enter Jev rescue.

## Important failure rule

If the initial Jev route call fails, generative must not silently decide the same
mutation route and proceed.

## Required tests

- simple jajan gorengan 5k -> exactly one Jev phase, zero generative;
- complex clear input -> route Jev + one generative, zero full semantic replay;
- one category residual -> one additional category rescue only;
- self-declared ambiguous generative result does not direct-confirm;
- Jev route failure exposes no mutation tool/authority;
- exact reply binding precedence remains unchanged;
- implicit open review does not swallow new expense.

## Telemetry expectation

Decision provenance must distinguish:

- JEV_ONLY;
- JEV_THEN_GENERATIVE;
- JEV_THEN_GENERATIVE_THEN_RESIDUAL_JEV.

The third state must identify the residual dimension.

---

# 9. IR-05 — Receipt residual bounded rescue

## Current audited shape

Clear new receipts can auto-confirm from generative extraction plus deterministic
conditions. This aligns with the new product decision and MUST NOT be regressed
into mandatory Jev replay.

Remaining product gap:

- if category is unresolved after vision extraction and deterministic merchant
  memory, user review may occur without first trying a cheap bounded category
  rescue.

## Required flow

~~~text
vision extraction
-> deterministic validation
-> duplicate search

if duplicate ambiguity:
  duplicate review

else if all required facts including category are safe:
  confirm directly
  Jev calls = 0

else if category is the only bounded residual:
  one Jev category rescue
  decisive -> confirm
  undecided/failure -> category-only review

else if evidence fact is missing:
  ask only missing evidence fact
~~~

## Forbidden implementation

Do not make every receipt category go through Jev.

Do not call Jev to verify a date that is absent.

Do not use upload time as transaction date.

## Required tests

- R-clear: vision 1, Jev 0, review 0;
- R-category-rescue: vision 1, Jev 1, review 0 when decisive;
- R-category-undecided: category-only review;
- R-date-missing: no Jev date guess, date-only review;
- R-duplicate: no new canonical duplicate.

## Exit gate

Jev on receipt exists only because it prevents a category review that would
otherwise be necessary.

---

# 10. IR-06 — Screenshot selective residual batch

## Current audited drift

ADR-042/current implementation direction uses one batched bounded category
ruling for unmatched rows. That is better than one call per row but still
re-judges rows whose vision category is already policy-acceptable.

## Required flow

~~~text
one vision call
-> validate all rows
-> partition:
   A clear
   B duplicate/reconciliation
   C residual bounded
   D missing evidence / human policy

A -> direct canonical path
B -> reconciliation
C -> one batched Jev rescue
D -> minimal review
~~~

Only partition C appears in Jev request.

## Required tests

20 clear rows:

~~~text
vision calls = 1
Jev calls = 0
reviews = 0
~~~

17 clear + 3 uncertain category rows:

~~~text
vision calls = 1
Jev calls = 1
Jev questions = 3 rows
~~~

not 20.

Mixed outcome:

- Jev resolves 2/3 -> 19 canonical, 1 category review;
- summary reflects canonical/linked/review counts;
- provenance on the 17 clear rows does not claim Jev.

## Exit gate

A decisive row never pays bounded judgment solely because another row is
uncertain.

---

# 11. IR-07 — Bank Email merchant-less category resolution

## Current audited drift

The deterministic policy can classify a merchant-less merchant-like expense as
UNKNOWN_MERCHANT before category Jev runs.

The UI/API no longer necessarily require merchant text, but the event still
becomes an avoidable category review.

## Required flow

~~~text
valid transaction evidence
merchant = NULL
category unresolved
-> category Jev
~~~

If decisive:

~~~text
CONFIRMED
merchant = NULL
~~~

If undecided/failure:

~~~text
NEEDS_REVIEW
missing_facts = ["category"]
merchant remains NULL
~~~

## Required tests

- merchant-less decisive category -> zero review;
- no merchant fabricated;
- undecided category -> category-only review;
- learned merchant deterministic path still wins and skips Jev;
- evidence verification behavior from PR #120 remains unchanged.

## Exit gate

Merchant absence alone never causes human work when merchant is not a canonical
requirement.

---

# 12. IR-08 — Payslip residual policy separation

## Current audited risk

A payslip review can combine missing date with salary-source policy selection in
UI even when household state already resolves the salary classification.

## Required semantics

Separate:

~~~text
financial evidence fact
vs
household policy choice
~~~

Cases:

### Date missing, primary salary already exists

~~~text
missing_facts = ["transaction_at"]
~~~

No salary selector.

### First salary source, all evidence complete

Ask only:

~~~text
PRIMARY_SALARY vs ORDINARY_INCOME
~~~

### First salary source + date missing

Represent both dimensions explicitly. The UI may resolve them in one minimal form
or sequentially, but RHICE must count the actual two inputs.

## Required tests

- existing primary + missing date = one field;
- no primary + clear date = one policy choice;
- no primary + missing date = two explicit unresolved dimensions;
- no model silently decides primary salary.

---

# 13. IR-09 — Intelligence-pass telemetry

## Purpose

Make model-order drift measurable.

## Required data

For each inference phase retain privacy-safe metadata:

- source_event_id or equivalent correlation;
- capability: JEV / GENERATIVE;
- purpose enum;
- semantic dimension(s);
- policy/model version;
- latency;
- outcome.

Suggested purpose enum:

- ROUTE
- TRANSACTION_BOUNDED
- EXTRACTION
- RESIDUAL_CATEGORY
- RESIDUAL_REVIEW_ACTION
- EVIDENCE_SUPPORT
- DOCUMENT_REPAIR
- OTHER_BOUNDED

## Derived metrics

Must support:

- Jev calls/event;
- generative calls/event;
- intelligence passes/event;
- residual rescue attempts;
- residual rescue success;
- redundant semantic double-pass rate;
- p50/p95 inference latency.

## Double-pass classification

Valid:

~~~text
GENERATIVE extraction
-> JEV residual category
~~~

Invalid/redundant:

~~~text
GENERATIVE accepted category
-> JEV category again
~~~

Telemetry must be able to distinguish these.

## Exit gate

A future product audit can answer:

~~~text
Which human reviews did Jev save?
Which model calls repeated work?
~~~

without reading raw prompts/evidence.

---

# 14. IR-10 — Regression, canary, and rollout hardening

## Required regression matrix

At minimum pin:

- simple Telegram no-generative fast path;
- complex Telegram no redundant full second Jev;
- residual Telegram category rescue;
- receipt clear / residual category / missing date / duplicate;
- screenshot all-clear / mixed residual batch / date missing / category conflict;
- merchant-less Bank Email;
- payslip policy separation;
- web + Telegram canonical review guards;
- RHICE actual-field counting.

## Semantic canary

Retain current Jev canary for independent evidence semantics.

Add fixtures only where model drift can change a bounded residual decision.

Do NOT create a canary that enforces model agreement for a clear generative fact
that no longer requires Jev.

## Rollback

Use independent switches where current config supports them.

Recommended separate rollback scopes:

- Telegram post-generative direct acceptance;
- receipt residual category rescue;
- screenshot selective residual batch;
- Bank Email merchant-less category auto-confirm.

## Exit gate

All required tests green, Hermes review addressed, and no drift-guard checklist
item unresolved.

---

# 15. /goals task graph

~~~text
IR-00
  |
  +--> IR-01 --> IR-02
  |               |
  |               +-------------------+
  |                                   |
  +--> IR-03 -------------------------+--> IR-09 --> IR-10
  |                                   |
  +--> IR-04 -------------------------+
  |                                   |
  +--> IR-05 -------------------------+
  |                                   |
  +--> IR-06 -------------------------+
  |                                   |
  +--> IR-07 -------------------------+
  |                                   |
  +--> IR-08 -------------------------+
~~~

Notes:

- IR-01/IR-02 should land before broad source behavior changes because they make
  review residuals safe.
- IR-03 measurement can land independently but should precede production
  interpretation of results.
- IR-04..IR-08 can be developed independently after baseline audit, but each must
  consume the same product/architecture contract.
- IR-09 should cover all source tasks before final rollout.
- IR-10 is the final integration gate.

---

# 16. Task completion template for Codex

Before marking any IR task complete, include:

~~~text
Task:
Baseline main SHA:
Files changed:
User interactions before:
User interactions after:
Jev calls before:
Jev calls after:
Generative calls before:
Generative calls after:
Canonical correctness guard:
Residual uncertainty after:
Tests added/updated:
Drift checklist:
  [pass/fail per item]
Known follow-up:
~~~

A task with unknown model-call ordering is not complete.

---

# 17. Final implementation invariant

Codex should be able to justify every extra user or model interaction with one
sentence:

~~~text
This interaction exists because ______ remains unresolved and it can reduce
residual uncertainty / protect an independent correctness claim.
~~~

If that sentence cannot be completed, remove the interaction.
