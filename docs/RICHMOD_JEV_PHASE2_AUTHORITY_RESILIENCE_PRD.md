# RICHMOD JEV PHASE 2 — AUTHORITY, RESILIENCE, AND EXPANSION PRD

**Status:** Ready for implementation  

> **Implementation status (updated in the same branch as the code).**
> Sprint A of the implementation order is implemented in `feat/jev-authority-resilience`:
> an explicit `TransactionSemanticDecision` now owns Telegram transaction
> confirmation, generative self-reported confidence is non-authoritative (it is
> treated as material ambiguity above the ambiguity ceiling and can never
> skip the bounded evaluator), both the harvested fast path and the
> post-extraction path consume one shared evaluator and policy version, the
> initial route/period call also carries the transaction sub-bundle when Go
> pre-harvested one amount candidate, already-loaded categories are passed
> through instead of re-queried, `JUDGMENT_MODEL` is required unless
> `JUDGMENT_MODE=disabled-dev`, a judgment outage degrades to a READ-only tool
> surface and refuses mutations, and bounded decision provenance is persisted in
> `judgment_decision` (migration 00056).
>
> Sprint B is implemented in `feat/jev-efficiency-provenance`:
> `record_transaction` now spends **one** System One round trip — the same
> bundled request decides direction, amount/date support, material ambiguity,
> and category, and the post-extraction path consumes that decision's category
> instead of issuing a second category-only call (the now-removed
> `resolveCategoryWithJudgment`). Deterministic confirmed merchant aliases still
> short-circuit the bounded call entirely via `exactMerchantCategory`. All
> thresholds and policy values moved into one versioned registry
> (`judgment_policy.go`, `judgmentPolicyVersion = 2026-09-jev2`), every bounded
> call is attributed to a named `judgmentTask`, and each consumed decision is
> counted with its product outcome so review/clarification rate per decision
> task and Jev-vs-generative share are measurable (§17/§18/§25).
>
> Sprint C is implemented in `feat/jev-transfer-semantics`:
> `record_transfer` no longer accepts a `purpose` argument at all. The generative
> model supplies only arbitrary facts (amount, source/destination hints, date/time),
> and Go resolves the canonical purpose *after* both sides are known, through the
> bounded `TRANSFER_PURPOSE` Choice (`resolveTransferPurpose`, PRD §13). The tool
> contract now mirrors the canonical rule instead of contradicting it: a missing
> destination hint is a plain internal transfer rather than a validation error,
> which was the actual reason an INTERNAL_TRANSFER transaction could be persisted
> with a related Wealth account. In-process callers that already know the purpose
> deterministically (a fixed asset-purchase reclassification) pass it as
> `reclassification_purpose`; that value is never read from model arguments.
> The `evaluate` telemetry wrapper also fails closed on an unconfigured judgment
> plane instead of dereferencing it.
>
> Still on the target design: the real LiteRouter → TypeSafe opt-in smoke (§12),
> the zero-generative transfer fast path (§14 — a transfer turn still needs the
> generative extractor to harvest arbitrary hints before Go can resolve them),
> duplicate bounded generative tool removal (§19), and Sprint D/E
> evidence-channel and insight work (§20–§22).
**Repository:** `raufimusaddiq/richmod`  
**Baseline:** `main@68fbd0fc73887b95983e8f460c4bd3d26eb34ca7`  
**Date:** 2026-09-22  
**Depends on:** PR #91, PR #93, PR #94, ADR-038  
**Scope:** Next requirements after native System One contract and initial Telegram decision-plane rollout

---

# 1. Product Decision

Jev is no longer an experiment or optional optimization in the Richmod product architecture.

For **bounded semantic decisions that can affect canonical financial state**, Richmod's intended authority chain is:

```text
deterministic Go
      |
      v
Jev / System One
      |
      v
Go policy
      |
      v
PostgreSQL / Review
```

A generative model may still extract arbitrary values or reason over open-ended context, but it must not regain final semantic authority merely because Jev is disabled, unavailable, or skipped.

The next implementation phase must therefore prioritize:

1. **single decision authority** — remove legacy LLM self-confidence from mutation approval;
2. **safe degradation** — Jev outage must not break deterministic READs, but must not reopen unsafe mutation fallback;
3. **fewer inference round trips** — bundle independent System One questions where the state is shared;
4. **transfer semantic ownership** — move bounded transfer purpose out of generative tool arguments;
5. **cross-channel expansion** — use Jev after arbitrary extraction in email/reconciliation workflows;
6. **decision observability** — make Jev's actual product value and policy decisions measurable.

This is more important than simply increasing Jev call count.

---

# 2. Latest-Main Product Audit

Latest main:

```text
68fbd0fc73887b95983e8f460c4bd3d26eb34ca7
Merge PR #94
feat(worker): native System One primitive contract and READ period correctness
```

## What is already correct

### Native System One protocol

PR #94 now models and decodes native System One primitives:

- Choice criteria maps;
- Choice probabilities/confidence;
- Noul probabilities;
- Score primitives;
- strict answer validation.

### Telegram READ routing

Route and reporting period are evaluated together.

`CUSTOM_OR_UNCLEAR` no longer silently becomes `THIS_MONTH`.

### Bounded server-state workflows

Jev owns ordinary bounded replies for:

- pending correction;
- pending batch confirmation/defer;
- salary choice;
- merchant-learning consent;
- fact-free review actions.

### Simple transaction fast path

A common explicit transaction can complete with:

```text
Go candidate harvesting
-> Jev type/category/support questions
-> Go validation
-> persistence
```

without a generative model.

### Category decision

Category is now resolved after deterministic merchant rules using Jev Choice.

These behaviors must be preserved.

---

# 3. Critical Gap — There Are Still Two Mutation Authorities

The current transaction persistence path still calls:

```go
shouldAutoConfirmTransaction(value, categoryFound)
```

which currently depends on:

```text
value.Confidence >= 0.90
value.CategoryConfidence >= 0.85
value.Ambiguous == false
```

This creates a second authority layer that conflicts with ADR-038.

## Example: Jev path

A simple transaction may already pass:

```text
amount_support Noul
date_support Noul
transaction_type Choice policy
category Choice policy
```

but its Jev Choice `confidence` is copied into the legacy field:

```text
validatedExtraction.Confidence
```

and is then judged again by the old `>= 0.90` rule.

That means:

```text
Jev policy says "safe"
      |
      v
legacy generic confidence says "not safe"
```

The policy is duplicated and semantically inconsistent.

## Example: generative fallback

The generative `record_transaction` path still emits:

```text
confidence
category_confidence
ambiguous
```

and these fields can still contribute to auto-confirm.

That leaves the original problem partially intact:

> the generative model may still propose a semantic answer and grade its own answer.

---

# 4. P0 — Introduce an Explicit Transaction Semantic Decision

Do not overload extraction fields to represent authority.

Add an explicit application-level decision object.

Suggested logical model:

```go
type TransactionSemanticDecision struct {
    RouteAccepted      bool

    TransactionType    string
    TypeAccepted       bool

    AmountSupported    bool
    DateSupported      bool

    CategorySlug       string
    CategoryAccepted   bool

    MaterialAmbiguity  bool

    Model              string
    PolicyVersion      string
    DecisionSource     string // JEV | DETERMINISTIC
}
```

The exact representation may differ, but the important distinction is:

```text
Extraction
!=
Semantic decision
!=
Canonical mutation authority
```

## New persistence rule

`persistTransaction` must not decide auto-confirm from generic model confidence.

Instead:

```text
Go structural validation
AND semantic decision policy passed
AND canonical/domain rules passed
      |
      +--> CONFIRMED
      |
      +--> NEEDS_REVIEW
```

The old confidence fields may remain temporarily for telemetry/schema compatibility, but they MUST be non-authoritative.

---

# 5. P0 — One Shared Transaction Decision Evaluator

Both paths must converge on the same semantic decision code.

## Path A — simple Jev-first transaction

```text
text
-> Go candidates
-> Jev decision bundle
-> TransactionSemanticDecision
-> Go persistence
```

## Path B — generative extraction fallback

```text
text
-> generative extraction of arbitrary values
-> Go structural validation
-> Jev decision bundle over source + proposal
-> TransactionSemanticDecision
-> Go persistence
```

Do not maintain separate auto-confirm semantics for Jev-first vs generative fallback.

Suggested service boundary:

```go
type TransactionDecisionInput struct {
    UserText   string
    Proposal   ExtractedTransaction
    Categories []CategoryCandidate
}

func EvaluateTransactionSemantics(
    ctx context.Context,
    requestID string,
    input TransactionDecisionInput,
) (TransactionSemanticDecision, error)
```

The same policy must be used regardless of how the arbitrary fields were obtained.

---

# 6. P0 — Generative Transaction Confidence Becomes Non-Authoritative

Current generative schemas still include:

```text
confidence
category_confidence
ambiguous
```

Migration:

## Step 1

Ignore `confidence` and `category_confidence` for mutation authority.

They may remain stored for compatibility and offline comparison only.

## Step 2

Stop asking the generative model for category confidence once category is Jev-owned.

## Step 3

Stop asking for one opaque overall transaction confidence.

## Step 4

Remove the fields entirely after no downstream consumer requires them.

The desired contract is:

```text
Generative model:
  arbitrary extraction only

Jev:
  bounded semantic support / classification / uncertainty

Go:
  decision policy
```

---

# 7. P0 — Jev Must Be Required for Production Mutation Semantics

Current production runbook still says:

```text
JUDGMENT_MODEL is optional
leave it unset to keep the existing conversational path
```

That is now inconsistent with ADR-038.

## Product requirement

For production:

```text
JUDGMENT_MODEL is required
```

for Jev-owned mutation semantics.

Do not make the entire worker unavailable if Jev is temporarily down.

Instead distinguish:

```text
configuration invariant
vs
runtime provider availability
```

### Configuration

Production deployment validation must reject a missing `JUDGMENT_MODEL`.

Local/dev may explicitly disable it.

Suggested explicit mode if needed:

```text
JUDGMENT_MODE=required
JUDGMENT_MODE=disabled-dev
```

Do not use an implicit missing env var as a production feature switch.

### Runtime

When configured Jev becomes unavailable:

- deterministic features continue;
- READ-only conversational capability may degrade safely;
- no Jev-owned automatic mutation may silently fall back to LLM semantic authority.

---

# 8. P0 — Safe Degradation During Jev Outage

Current initial Telegram Jev route failure can terminate the turn with an `IGNORED` response asking the user to restate.

That makes Jev a single point of failure for ordinary READ UX.

At the same time, simply falling back to the full generative agent would be unsafe because side-effect tools could bypass Jev.

## Required degraded mode

When the initial judgment call fails because of transport/provider failure:

```text
Telegram
  |
  v
judgment unavailable
  |
  +--> generative agent with READ-only tool surface
  |
  +--> help / clarification
  |
  X--> no mutation side-effect tools
```

Suggested state flag:

```text
judgment_status = UNAVAILABLE
```

Then construct the agent tool catalog accordingly.

Allowed:

```text
query_spending
query_cashflow
query_savings
query_wealth
search_transactions
list_review_items
finance_help
ask_clarification
```

Disallowed while Jev-owned semantic mutation is unavailable:

```text
record_transaction
record_transfer
resolve semantic review actions
salary semantic choice
merchant learning
semantic correction action
```

The reply for a mutation request should explain that the request was **not recorded**, rather than pretending success or silently using LLM confidence.

---

# 9. P0 — Distinguish Semantic Uncertainty From Infrastructure Failure

These are different product states.

## Semantic uncertainty

Examples:

```text
Choice margin too small
Noul in the middle band
OTHER_OR_UNCLEAR
```

Outcome:

```text
clarification
or
Review Inbox
```

## Infrastructure failure

Examples:

```text
timeout
LiteRouter unavailable
TypeSafe unavailable
invalid upstream response
```

Outcome depends on operation class:

### READ

```text
degrade to generative READ-only orchestration
```

### mutation

```text
do not auto-mutate
preserve evidence/proposal when useful
surface retry/review state
```

Telemetry must distinguish these two classes.

---

# 10. P0 — Collapse the Simple Transaction Into One System One Round Trip

Current common flow may be:

```text
System One #1
  route + period

then

System One #2
  type + category + amount/date support
```

TypeSafe's intended workflow shape is many independent narrow questions over shared state, evaluated in parallel.

For a message where Go can already harvest one transaction candidate before the first call, include the transaction sub-bundle in the initial request.

## Target

```text
Go pre-harvest
  |
  +--> one amount?
  +--> date candidate?
  +--> active categories?
  |
  v
ONE System One request
  |
  +--> route
  +--> period
  +--> transaction_type
  +--> amount_support
  +--> date_support
  +--> category
  +--> material_ambiguity
```

If route resolves to `CREATE_TRANSACTION`, consume the already-returned transaction answers.

No second Jev round trip.

If the route is unrelated, ignore the speculative transaction answers.

---

# 11. P0 — Reuse Already-Loaded Categories

`ProcessAgent` already loads active category slugs before entering the fast path.

Do not re-query category slugs inside the simple transaction path merely because it is implemented in a separate function.

Pass the already-loaded server context into the initial judgment bundle.

Benefits:

- one fewer database query;
- simpler deterministic snapshot;
- route/category decisions see one consistent category set;
- easier request auditing.

---

# 12. P0 — Add a Real End-to-End System One Smoke

Unit tests now correctly validate native request/answer contracts, but the product still needs a real interoperability check:

```text
Richmod
-> LiteRouter
-> TypeSafe
-> LiteRouter
-> Richmod
```

This is not a unit test.

Add an opt-in smoke command/test for an approved secret-bearing environment.

Verify:

- LiteRouter client authentication;
- real native Choice;
- real native Noul;
- model routing;
- actual returned model version;
- Richmod strict decoder;
- request timeout behavior.

This must not commit TypeSafe credentials.

---

# 13. P1 — Transfer Purpose Must Move Out of the Generative Tool Contract

**Implemented.** `record_transfer` no longer has a `purpose` argument; the schema
and the typed decoder both reject it, and a regression test asserts the contract
stays purpose-free. The generative model supplies only arbitrary facts, and
`resolveTransferPurpose` decides the canonical value through a bounded
`TRANSFER_PURPOSE` Choice over the server-owned candidate set.

Before it was removed, `record_transfer` asked the generative model to emit:

```text
purpose:
  SAVINGS_TRANSFER
  INVESTMENT_CONTRIBUTION
  ASSET_PURCHASE
  DEBT_PRINCIPAL_PAYMENT
  INTERNAL_TRANSFER
```

That is a bounded semantic Choice.

## Target flow

```text
user text
  |
  v
Go exact account / alias / reconciliation rules
  |
  +--> purpose determined exactly
  |
  +--> unresolved
           |
           v
       Jev Choice
```

Jev Choice:

```text
SAVINGS_TRANSFER
INVESTMENT_CONTRIBUTION
ASSET_PURCHASE
DEBT_PRINCIPAL_PAYMENT
INTERNAL_TRANSFER
OTHER_OR_UNCLEAR
```

The generative model may still extract arbitrary:

- source-account hint;
- destination-account hint;
- description;
- date/time;

when deterministic candidates are insufficient.

Go's `transfer_wealth_compatible` remains final authority.

---

# 14. P1 — Zero-Generative Transfer Fast Path

For common explicit transfers where Go can harvest:

```text
amount
source account alias
destination wealth/account alias
date reference
```

Jev should be able to decide purpose without invoking the generative model.

Example:

```text
"transfer 2 juta dari Jago ke Bibit"
```

Potential path:

```text
Go candidate harvesting
-> exact account candidates
-> Jev transfer-purpose Choice
-> Go compatibility/reconciliation
-> mutation/review
```

This mirrors the transaction fast path.

Do not implement brittle provider-specific account parsing.

---

# 15. P1 — Decision Provenance Must Be First-Class

`llm_call` is operational telemetry.

It should not become the only record of why a financial decision happened.

Add bounded decision provenance.

Preferred new table:

```text
judgment_decision
-----------------
id
household_id
source_event_id nullable
task
model
policy_version
question_keys
answer_summary_json
outcome
created_at
```

`answer_summary_json` contains only bounded decision values/probabilities/confidence required for audit.

Do NOT store:

- raw Telegram text;
- raw email body;
- document bytes;
- full System One request state;
- provider credentials.

Example:

```json
{
  "transaction_type": {
    "choice": "EXPENSE",
    "top_probability": 0.96,
    "confidence": 0.91
  },
  "amount_support": {
    "noul": 0.99
  },
  "category": {
    "choice": "transport",
    "top_probability": 0.89,
    "margin": 0.31
  }
}
```

Final financial authority remains represented by the existing audit log / canonical record.

---

# 16. P1 — Separate Operational Telemetry From Decision Audit

Keep `llm_call` focused on:

```text
protocol
task
model
latency
tokens
cost
status
error
```

Use `judgment_decision` for:

```text
what semantic decision was consumed
which policy consumed it
what product outcome followed
```

Where practical, both records should share a request/correlation ID.

---

# 17. P1 — Measure Jev's Product Value

Add metrics that directly answer:

> Is Jev actually reducing generative work and review burden?

Required dimensions:

```text
jev_only
jev_then_generative
generative_only
judgment_unavailable
semantic_uncertainty
```

Required counters:

```text
telegram_turns_total
telegram_turns_jev_only
telegram_turns_jev_then_generative
telegram_turns_generative_only

telegram_mutations_jev_only
telegram_mutations_after_generative_extraction

generative_tool_calls_avoided
judgment_calls
judgment_error_rate

review_rate_by_decision_task
clarification_rate_by_decision_task
```

Do not set success targets until enough production traffic exists.

First measure the baseline.

---

# 18. P1 — Version Judgment Policy

Current named policy variables are an improvement, but the values are not yet externally identifiable in audit.

Introduce a policy version:

```text
JEV_POLICY_VERSION=2026-09-v1
```

or an equivalent compile-time constant.

Every persisted `judgment_decision` must carry the version.

Thresholds may remain in code initially.

The requirement is reproducibility:

```text
model version
+
policy version
+
bounded answers
=
explainable Go decision
```

---

# 19. P1 — Remove Duplicate Generative Tools After Production Ownership

PR #93 / #94 intercept several bounded workflows before the agent, but their generative tools remain available as fallback.

Once production Jev is required and degraded-mode behavior is implemented, remove or narrow duplicate bounded tools.

Candidates:

```text
resolve_salary_choice
resolve_merchant_learning
simple pending-action confirm/cancel
simple pending-batch confirm/cancel/defer
fact-free review action selection
category semantic authority
```

Keep generative fallback tools only when they carry arbitrary values that Jev cannot generate.

This reduces:

- prompt size;
- tool ambiguity;
- validation code;
- accidental duplicate authority;
- schema retries.

---

# 20. P2 — Bank Email: Jev After Arbitrary Extraction

Bank Email currently still relies on a generative extractor and a generic `extraction.Confidence < 0.80` gate.

The arbitrary extraction itself is reasonable.

The semantic verification is not yet aligned with ADR-038.

## Target

```text
trusted bank email
  |
  v
generative arbitrary extraction
  |
  v
Go structural validation
  |
  v
Jev verification bundle
  |
  +--> transaction_observed Noul
  +--> amount_supported Noul
  +--> direction_supported Noul
  +--> channel_supported Noul
  +--> material_ambiguity Noul
  |
  v
Go bank policy / reconciliation
```

The original email text and extracted proposal may be included in minimized Jev state.

Do not ask Jev to generate merchant/date strings.

Replace generic extraction confidence as policy paths migrate.

---

# 21. P2 — Financial Provider Email: Move Kind/Movement Semantics to Jev

Current provider-email processing still uses generative:

```text
observation.Kind
MovementType
Confidence
```

and `planCash` rejects when:

```text
v.Confidence < 0.8
```

This is another self-confidence gate.

Target responsibility:

## Generative model

Extract arbitrary:

- amount/value;
- time/date;
- account hints;
- provider reference;
- textual description.

## Jev

Bounded decisions:

```text
observation_type Choice
cash_movement_supported Noul
wealth_value_supported Noul
movement_type Choice
evidence_sufficient Noul
material_ambiguity Noul
```

## Go

- resolve accounts;
- validate money/time;
- apply `transfer_wealth_compatible`;
- reconcile;
- mutate/review.

---

# 22. P2 — Semantic Reconciliation After Hard Filtering

Do not replace deterministic reconciliation.

Use Jev only when hard rules leave a small ambiguous candidate set.

Example:

```text
Go filters:
  same household
  compatible amount
  compatible direction
  compatible time window
  compatible account

2 candidates remain
      |
      v
Jev Noul per candidate:
  same_real_event?
      |
      v
Go threshold + margin
```

Jev never receives authority to search arbitrary canonical rows.

---

# 23. P2 — Insight Signal Selection

Current insight generation always invokes the generative model once data completeness is sufficient.

Before prose generation, Jev can determine whether there is anything semantically noteworthy.

Potential questions:

```text
spending_change_material?            Noul
category_shift_material?             Noul
merchant_concentration_noteworthy?   Noul
cashflow_pattern_noteworthy?         Noul
savings_pattern_noteworthy?          Noul
primary_insight_family               Choice
```

Then:

```text
no meaningful signal
-> deterministic concise response

meaningful signal
-> generative model writes prose from selected facts
```

The generative model should write language, not decide whether already-computed metrics matter when the question is bounded.

---

# 24. P3 — Document Judgment Only When Evidence Is Text-Backed

The document vision model still emits:

- document type confidence;
- field confidence;
- missing/ambiguous fields.

Do not rush Jev into this path merely for coverage.

Jev does not see pixels.

Use it only when a reliable text/structured evidence layer exists and the questions are genuinely supported by that state.

Potential later uses:

```text
document family Choice
field support Noul
evidence sufficiency Noul
review reason Choice
```

Vision extraction itself remains generative.

---

# 25. Product Failure Matrix

| Capability | Jev available | Jev uncertain | Jev unavailable |
| --- | --- | --- | --- |
| Deterministic READ | normal | normal | normal |
| Semantic READ routing | Jev route | clarification / generative READ | generative READ-only fallback |
| Simple transaction | Jev + Go | Review / clarification | no auto mutation |
| Generative transaction extraction | LLM extract -> Jev -> Go | Review / clarification | no auto mutation |
| Bound review mutation | Jev -> Go | clarification | no mutation |
| Merchant consent | Jev Noul | clarification | no rule creation |
| Transfer purpose | Jev / deterministic Go | Review | no semantic auto mutation |
| Bank/provider email | extract -> Jev -> Go | Review | retry / Review |
| Insight prose | Jev select -> optional LLM | conservative output | deterministic fallback |

This matrix is part of the product contract.

---

# 26. Updated System Architecture

```text
                         RICHMOD

                  source / user message
                           |
                           v
              Go deterministic context
             + candidate harvesting
                           |
                           v
                 bounded decisions?
                  /             \
                no               yes
                |                 |
                |                 v
                |        LiteRouter /systemone
                |                 |
                |                Jev
                |                 |
                |      typed probabilities/confidence
                |                 |
                |          Go policy engine
                |                 |
                |         enough information?
                |           /          \
                |         yes           no
                |          |             |
                |          |             v
                |          |      generative model
                |          |   arbitrary extraction /
                |          |    reasoning / prose
                |          |             |
                |          +-------> Jev again only
                |                   for new bounded
                |                   decisions
                |
                +-------------------------+
                          |
                          v
                 Go canonical validation
              auth / bind / reconcile / math
                    /                 \
                mutate                Review
                   |
                   v
               PostgreSQL
```

---

# 27. Implementation Order

## Sprint A — Authority and resilience

1. add explicit `TransactionSemanticDecision`;
2. remove generic LLM confidence from auto-confirm authority;
3. run the same Jev transaction semantic evaluator after generative extraction;
4. require Jev configuration for production mutation semantics;
5. implement judgment-unavailable READ-only degraded agent mode;
6. define semantic-uncertainty vs provider-failure handling;
7. add real LiteRouter → TypeSafe smoke;
8. update ADR-038/runbook for production requirement.

## Sprint B — Efficiency and provenance

1. collapse simple transaction route + semantic questions into one System One call;
2. reuse already-loaded category context;
3. add `judgment_decision` migration and schema docs;
4. add policy version;
5. add Jev-vs-generative product metrics;
6. remove duplicate bounded generative tools where safe.

## Sprint C — Transfer semantics

1. move transfer purpose to Jev;
2. keep account/arithmetic/compatibility deterministic;
3. add zero-generative transfer fast path where candidates are sufficient;
4. add transfer decision provenance/tests.

## Sprint D — Evidence channels

1. Bank Email verification bundle;
2. Financial Provider Email bounded classification;
3. remove generative confidence from their policy authority;
4. semantic reconciliation after deterministic candidate narrowing.

## Sprint E — Intelligence optimization

1. Jev insight signal selection;
2. optional document post-vision bounded judgments when evidence permits.

---

# 28. Required Tests

## Authority

- simple Jev transaction does not depend on legacy `Confidence >= 0.90`;
- generative transaction confidence cannot auto-confirm by itself;
- category confidence from the generative model cannot authorize mutation;
- both simple and generative extraction paths use the same semantic decision policy;
- Jev uncertainty routes Review/clarification.

## Configuration

- production validation rejects missing `JUDGMENT_MODEL`;
- dev-disabled mode is explicit;
- runtime provider outage does not disable deterministic functionality.

## Outage behavior

- Jev route outage + READ request can still use READ-only generative fallback;
- Jev route outage cannot expose mutation tools;
- Jev outage after generative extraction creates no confirmed mutation;
- user receives an explicit non-success response;
- source evidence is preserved.

## Bundling

- one harvestable simple transaction performs one initial System One call;
- route/type/category/support answers share one state snapshot;
- non-transaction turns ignore speculative transaction answers safely.

## Transfer

- deterministic exact transfer bypasses Jev when semantics are already exact;
- ambiguous transfer purpose uses Jev Choice;
- OTHER_OR_UNCLEAR routes review;
- Go compatibility can reject a high-confidence Jev purpose.

## Email

- generative confidence alone cannot auto-accept bank/provider email semantics;
- Jev verifies bounded claims after extraction;
- provider outage routes retry/review;
- hard deterministic rules remain authoritative.

## Provenance

- every Jev-influenced mutation/review can identify model version;
- every decision carries policy version;
- no raw financial message is stored in `judgment_decision`.

---

# 29. Acceptance Criteria

Phase 2 is complete when:

1. no generative self-reported confidence directly authorizes a canonical transaction;
2. simple and generative transaction paths converge on one Jev semantic decision policy;
3. production cannot silently disable Jev-owned mutation semantics by omitting `JUDGMENT_MODEL`;
4. Jev outage preserves READ availability without allowing mutation fallback;
5. common simple transactions need only one System One round trip and zero generative calls;
6. transfer purpose is Jev-owned whenever deterministic Go cannot decide;
7. Jev decision provenance is auditable without storing raw source content;
8. metrics show how many generative calls/tool calls are avoided;
9. Bank Email and Financial Provider Email no longer use generative confidence as final semantic authority;
10. semantic reconciliation uses Jev only after deterministic narrowing;
11. insight generation can skip generative prose when Jev finds no meaningful signal;
12. Go/PostgreSQL remain the final canonical authority.

---

# 30. Product Success Definition

The success metric is not:

```text
"number of Jev calls"
```

The target is:

```text
fewer generative calls
+
smaller generative tool surface
+
fewer self-confidence gates
+
lower latency for common commands
+
safe automation with explicit uncertainty
+
less unnecessary Review Inbox work
```

while preserving deterministic financial correctness.

---

# 31. External Design References

The design follows TypeSafe's current public framing of System One workflows:

- use narrow Noul / Choice / Score questions;
- let code own deterministic rules;
- decompose workflows rather than putting the entire policy into one prompt;
- multiple questions over shared state are evaluated as structured decisions;
- use probabilities/confidence so software can choose automation vs review.

References:

- `https://api.typesafe.ai/docs`
- `https://typesafe.ai/blog/introducing-system-one-models-and-jev`
- `https://evals.typesafe.ai/`

Provider documentation remains authoritative for native protocol details.
