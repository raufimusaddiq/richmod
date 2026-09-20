# RICHMOD JEV / SYSTEM ONE INTEGRATION PRD

**Status:** Ready for implementation — direct production integration  
**Repository:** `raufimusaddiq/richmod`  
**Target branch:** `RICHMOD`  
**Implementation branch:** `docs/jev-system-one-prd`  
**Baseline:** `main@81e466ca0f71c8c056dd2c500f4ca17fb4466c51`  
**Date:** 2026-09-20  
**Gateway dependency:** LiteRouter `POST /v1/systemone` available on `main` via merge commit `fd48eac93f2464a38d47a39106fd9c5b47e83e38`

---

# 1. Product Goal

Richmod will use TypeSafe AI Jev as its primary **bounded semantic decision plane**.

The goal is not merely to add a second model behind existing generative workflows. The goal is to reduce unnecessary generative-model calls, native-tool parsing, self-reported confidence, prompt complexity, and hand-written fuzzy branching wherever the required result can be represented as a typed decision.

Target responsibility split:

```text
Go deterministic rules
  = exact validation, authorization, binding, arithmetic, reconciliation, persistence

Jev / System One
  = bounded semantic classification, routing, verification, scoring, uncertainty

Generative LLM
  = arbitrary string/value extraction, open-ended reasoning, vision, synthesis, prose

PostgreSQL
  = canonical financial truth
```

The preferred execution order is:

```text
deterministic Go
      |
      v
bounded semantic decision needed?
    /   \
   no   yes
   |     |
   |    Jev
   |     |
   |     v
   |  enough information to finish?
   |       /       \
   |     yes       no
   |      |         |
   |      |     generative LLM
   |      |   extraction/reasoning
   |      |         |
   |      |    Jev when another
   |      |    bounded judgment remains
   |      |         |
   +------+---------+
          |
          v
       Go policy
       /      \
   commit     review
```

Jev is therefore not only a guard after the LLM. It is allowed to **replace the LLM entirely** for bounded decision tasks.

This rollout is direct production integration, not shadow mode.

---

# 2. Architecture Invariants

Implementation MUST preserve:

1. PostgreSQL remains canonical financial state.
2. Go owns every canonical financial state transition.
3. Exact deterministic facts are never delegated to AI.
4. Jev output is typed semantic evidence, not mutation authority.
5. Generative LLM output remains untrusted.
6. No AI model receives database credentials or direct database access.
7. Exact Telegram callbacks, reply bindings, opaque references, and household authorization remain Go-owned.
8. Deterministic reconciliation and arithmetic take precedence over model judgment.
9. Ambiguity that cannot safely resolve routes to Review Inbox or clarification.
10. All external model inference goes through LiteRouter.
11. Richmod never stores or uses the upstream TypeSafe credential.
12. No Python service or sidecar is introduced.

This PR adds ADR-038 to formalize this decision and amends the existing gateway documentation.

---

# 3. Gateway Boundary

LiteRouter already exposes native System One pass-through:

```text
POST /v1/responses
POST /v1/chat/completions
POST /v1/systemone
```

Richmod uses the same gateway boundary for both model families:

```text
Richmod
   |
   +--> LiteRouter /v1/responses
   |        |
   |        +--> generative providers
   |
   +--> LiteRouter /v1/systemone
            |
            +--> TypeSafe Jev
```

Important:

- Richmod does not call `api.typesafe.ai` directly.
- Richmod does not possess `TYPESAFE_API_KEY`.
- LiteRouter owns upstream provider credentials, account fallback, provider health, and routing.
- System One keeps its native payload. It is not translated to OpenAI Responses.
- Jev is not represented as a fake chat model.
- No automatic fallback from Jev to a generative model is permitted because their semantics differ.

LiteRouter currently accepts the native TypeSafe request shape:

```json
{
  "model": "typesafe/jev-latest",
  "state": {},
  "questions": {}
}
```

and passes the structured response through.

---

# 4. Why Jev Changes the Richmod Design

Richmod currently uses native generative tool calls in several places for two different jobs:

1. **unbounded extraction** — amount, date, merchant, arbitrary text, document fields;
2. **bounded decisions** — choose transaction type, category, review action, salary choice, transfer purpose, yes/no confirmation, ambiguity.

The first job still fits a generative model.

The second job fits Jev better because the output space is already known to software.

Example of the current pattern:

```text
LLM native tool:
resolve_salary_choice {
  choice: "PRIMARY"
}
```

Desired pattern:

```text
Jev Choice:
PRIMARY
ORDINARY
IGNORE
OTHER_OR_UNCLEAR
```

There is no reason to invoke a generative model, generate a tool call, parse JSON arguments, validate the tool name, and decode a struct just to choose one member of a finite set.

The same reasoning applies to many other Richmod decisions.

---

# 5. Decision Hierarchy

Every semantic workflow MUST follow this hierarchy.

## Level 1 — deterministic Go

Use Go when the answer can be computed exactly.

Examples:

- household authorization;
- exact Telegram reply binding;
- callback identity;
- amount arithmetic;
- date interval arithmetic;
- category IDs/slugs that are already explicit;
- known account alias resolution when unique;
- duplicate hashes;
- transaction status transition validity;
- exact reconciliation rules;
- salary-cycle boundaries;
- canonical totals and analytics.

Do not ask Jev or an LLM for these.

## Level 2 — Jev

Use Jev when:

- the possible answers are known;
- the problem is semantic or fuzzy;
- a probability or distribution is useful;
- arbitrary string generation is not required.

Examples:

- classify intent;
- choose one review action;
- choose category;
- choose transfer purpose;
- decide whether two descriptions likely represent the same event;
- decide whether evidence supports a proposed fact;
- decide whether a user explicitly consented;
- classify finance vs out-of-scope;
- rank review urgency;
- choose which bounded workflow should run.

## Level 3 — generative LLM

Use the generative model only when Richmod needs something Jev cannot natively return:

- arbitrary merchant names;
- arbitrary descriptions;
- arbitrary dates/times not captured by deterministic candidates;
- free-form document fields;
- image/vision understanding;
- multi-step READ reasoning;
- open-ended financial explanation;
- user-facing prose.

After the generative model returns an extraction/proposal, any remaining bounded semantic choice should return to Jev rather than relying on generative self-confidence.

---

# 6. Jev-First Telegram Fast Path

Telegram should no longer assume every free-text finance message must enter the full conversational LLM agent.

New flow:

```text
Telegram text
    |
    v
Go context + candidate harvesting
    |
    v
Jev routing / semantic questions
    |
    +--> bounded READ fully resolvable
    |       -> Go READ
    |       -> deterministic concise response
    |
    +--> bounded mutation fully resolvable
    |       -> Go validation
    |       -> Go mutation/review
    |
    +--> server-bound workflow reply
    |       -> Jev chooses allowed action
    |       -> Go executes bound transition
    |
    +--> needs arbitrary extraction/reasoning/prose
            -> existing conversational LLM agent
            -> Jev only for remaining bounded judgments
            -> Go
```

This is the main architectural change.

Jev should be used to avoid generative calls, not merely to add another call after them.

---

# 7. Telegram Route Decision

For ordinary free text, Jev may answer a `Choice` over a server-owned route set:

```text
READ_SPENDING
READ_CASHFLOW
READ_SAVINGS
READ_WEALTH
SEARCH_TRANSACTIONS
CREATE_TRANSACTION
CREATE_TRANSFER
CORRECT_TRANSACTION
REVIEW_INTERACTION
SALARY_INTERACTION
MERCHANT_LEARNING_INTERACTION
FINANCE_HELP
OUT_OF_SCOPE
NEEDS_GENERATIVE_AGENT
OTHER_OR_UNCLEAR
```

The available choices are state-dependent.

For example, `SALARY_INTERACTION` is exposed only when a salary choice is actually pending.

Go decides which routes are available. Jev only chooses among them.

If route probability/margin is insufficient, send the turn to the generative conversational agent or ask clarification, depending on available context.

---

# 8. Candidate-Based Extraction Before Generative LLM

Jev cannot produce arbitrary strings. Richmod should exploit that constraint instead of treating it as a weakness.

Before invoking a generative model, Go may harvest **candidates** without deciding semantics:

```text
amount candidates
date-reference candidates
time candidates
known account aliases
known merchant aliases
opaque transaction refs
active category slugs
review actions
transfer purposes
```

Example:

```text
"kemarin makan 50rb sama parkir 5rb"
```

Go can harvest:

```text
amount_candidates = [50000, 5000]
date_candidate = YESTERDAY
```

Jev can then answer bounded questions about which amount belongs to which candidate transaction when the structure is sufficiently clear.

If candidate harvesting is incomplete or arbitrary extraction is required, fall through to the generative LLM.

This allows a large common subset of Telegram commands to avoid native-tool LLM calls.

---

# 9. Replace Bounded Native Tool Decisions With Jev

The following current native-tool decisions are candidates to move out of the generative conversational agent.

## 9.1 Review resolution

Current:

```text
LLM -> resolve_review(action=...)
```

Target:

```text
Go binds exact review
   |
   v
Go exposes only allowed actions
   |
   v
Jev Choice over allowed actions + OTHER_OR_UNCLEAR
   |
   v
Go validates and executes
```

No generative tool call is required for replies such as:

- "iya expense aja";
- "itu transfer ke rekening sendiri";
- "abaikan";
- "jadikan asset purchase".

The target review remains server-owned.

## 9.2 Salary choice

Current tool:

```text
resolve_salary_choice
```

Target Jev Choice:

```text
PRIMARY
ORDINARY
IGNORE
OTHER_OR_UNCLEAR
```

## 9.3 Merchant-learning reply

Current tool:

```text
resolve_merchant_learning { remember: boolean }
```

Target Jev Noul:

```text
did_user_explicitly_consent_to_remember_rule?
```

A high probability may map to true; a low probability to false; uncertainty asks clarification.

The explicit opt-in requirement remains.

## 9.4 Pending batch decision

Confirmation/cancel/update/defer is a bounded state-machine decision.

Use deterministic lexical shortcuts first, then Jev Choice when natural language is not exact.

## 9.5 Finance scope

Use Jev to distinguish finance request vs non-finance/out-of-scope when deterministic routing cannot decide.

---

# 10. Transaction Creation

Transaction creation has both bounded and unbounded components.

Bounded:

```text
transaction intent
INCOME vs EXPENSE
category
is amount candidate supported?
is date candidate supported?
material ambiguity
```

Potentially unbounded:

```text
merchant string
free-form description
arbitrary date text
multiple transaction decomposition
```

Preferred path:

```text
Go harvest candidates
   |
   v
Jev:
- is this a record-transaction request?
- INCOME or EXPENSE?
- which amount candidate?
- which category?
- which date-reference candidate?
- material ambiguity?
   |
   v
all required values available?
   | yes
   v
Go directly records/stages transaction
```

Only invoke the generative LLM when required values cannot be obtained from deterministic candidates and Jev choices.

For a simple command such as:

```text
"catat bensin 50rb hari ini"
```

the target is **zero generative-model calls**.

---

# 11. Transaction Category

Category classification becomes Jev-owned after deterministic rules.

Order:

```text
explicit category from user
    -> use it

existing explicit merchant rule
    -> use it

otherwise
    -> Jev Choice
```

Choice includes every active allowed category plus:

```text
OTHER_OR_UNCLEAR
```

Initial Go acceptance policy:

```text
winner != OTHER_OR_UNCLEAR
AND top_probability >= configured minimum
AND top_probability - second_probability >= configured margin
```

Generative `category_confidence` is removed from decision authority and may eventually be removed from the tool schema entirely.

---

# 12. Transfer Purpose and Accounting Semantics

Transfer semantics are a strong Jev use case.

Deterministic rules run first:

- exact own-account match;
- exact known household account;
- exact reconciliation candidate;
- explicit user-specified purpose.

If unresolved, Jev Choice may classify among:

```text
SAVINGS_TRANSFER
INVESTMENT_CONTRIBUTION
ASSET_PURCHASE
DEBT_PRINCIPAL_PAYMENT
INTERNAL_TRANSFER
ORDINARY_EXPENSE
OTHER_OR_UNCLEAR
```

Go still validates whether the chosen semantic class is legal for the bound accounts and transaction state.

This can reduce review volume without moving canonical accounting authority into the model.

---

# 13. Corrections

When a correction target is already bound through:

- exact reply;
- opaque ref;
- unique search result;
- pending action;

Jev may interpret the **bounded semantic change** directly.

Examples:

```text
change category to transport
mark as internal transfer
ignore this
use yesterday
```

Arbitrary replacement text still requires deterministic extraction or the generative agent.

Jev never selects hidden transaction IDs.

---

# 14. Bank Email

Bank email still requires arbitrary observed-fact extraction, so the generative model remains useful there.

However the current extractor should stop asking the generative model to judge its own confidence wherever Jev can do it.

Target:

```text
trusted email
   |
   v
generative extraction
(amount, time, merchant, channel, description)
   |
   v
Go structural validation
   |
   v
deterministic reconciliation
   |
   v
Jev bounded semantic workflow
   |
   +--> evidence supports amount?
   +--> evidence supports direction?
   +--> evidence supports channel?
   +--> counterparty semantics?
   +--> transfer purpose/accounting family if unresolved?
   +--> material ambiguity?
   |
   v
Go commit/proposal/review
```

The generative model should not emit authoritative confidence values for these decisions once Jev owns them.

Where generic deterministic candidate extraction can reliably identify an exact amount/date from the email without bank-specific parsing, implementation may skip the generative extractor for that field. Do not add bank-specific production branches.

---

# 15. Financial Provider Email

Jev should be the default classifier for bounded provider-email decisions after any necessary arbitrary extraction.

Candidate questions:

```text
observation_type
evidence_sufficient
is_balance_observation
is_trade_or_asset_purchase
is_cashflow_event
likely_same_event_as_candidate
material_ambiguity
review_reason
```

The generative model is used only for fields that require arbitrary textual extraction.

---

# 16. Reconciliation

Do not replace exact deterministic reconciliation with AI.

Jev may be used only for unresolved semantic similarity after deterministic filters narrow the candidates.

Example:

```text
Go:
same household
compatible amount
compatible time window
compatible direction
candidate count = 2

Jev Noul for each:
does this evidence likely describe the same real event?

Go:
accept only if policy threshold and margin pass
otherwise review
```

This can reduce brittle hand-written text similarity rules while preserving exact financial constraints.

---

# 17. Review Inbox Triage

Jev should produce structured review metadata rather than requiring an LLM to write classification JSON.

Candidate Choice:

```text
AMOUNT_UNSUPPORTED
TYPE_AMBIGUOUS
CATEGORY_AMBIGUOUS
ACTION_INTENT_AMBIGUOUS
EVIDENCE_INSUFFICIENT
COUNTERPARTY_AMBIGUOUS
TRANSFER_SEMANTICS_AMBIGUOUS
RECONCILIATION_AMBIGUOUS
OTHER
```

Optional Score may be used for review urgency only if the product needs prioritization.

The UI can render deterministic explanations from Jev probabilities.

No generative prose is required for routine review reasons.

---

# 18. Document Pipeline

Jev cannot replace vision understanding of an image.

The generative vision model remains responsible for arbitrary visual extraction.

But the current document contract asks the same generative model for:

- document type;
- per-field status;
- per-field confidence;
- missing/ambiguous fields.

Those are bounded judgments that should migrate to Jev **when Jev receives sufficient text/structured evidence**.

Long-term target:

```text
image
  |
  v
vision model
  |
  +--> observed arbitrary values / text only
  |
  v
Jev
  |
  +--> document type
  +--> field support
  +--> field ambiguity
  +--> evidence sufficiency
  +--> review reason
  |
  v
Go
```

Do not claim Jev verified pixels it never received.

Until a reliable text-backed representation exists, keep the current vision confidence path for document-only facts.

---

# 19. Insights and Analytics

Canonical numbers always come from Go/PostgreSQL.

Jev may reduce generative insight work by deciding which structured observations are noteworthy before prose generation.

Examples:

```text
spending_change_material?
category_shift_material?
merchant_concentration_noteworthy?
cashflow_pattern_noteworthy?
savings_pattern_noteworthy?
which bounded insight family is most relevant?
```

Then:

```text
Go aggregates
   |
   v
Jev chooses relevant signals
   |
   +--> no notable signal -> deterministic response
   |
   +--> notable signals -> LLM writes concise explanation
```

This keeps the LLM focused on language rather than classification.

---

# 20. System One Question Design

Prefer many narrow independent questions over one opaque question.

Good:

```text
amount_supported?
transaction_type?
category?
explicit_user_consent?
material_ambiguity?
same_real_event?
```

Avoid:

```text
safe_to_commit?
```

because it hides domain policy inside the model.

## Noul

Use for independent yes/no judgments:

```text
amount_supported
action_matches_user_intent
evidence_sufficient
material_ambiguity
same_real_event
explicit_consent
```

A value near 0.5 is uncertainty.

## Choice

Use for one answer from a known set:

```text
route
transaction_type
category
transfer_purpose
review_action
review_reason
salary_choice
```

When the list may not contain the correct answer, include `OTHER_OR_UNCLEAR`.

## Score

Use only for genuinely ordered qualitative judgments such as review urgency or insight materiality.

Do not use Score as fake exact arithmetic.

---

# 21. Go Candidate Harvesting

Go is allowed to generate candidate sets because candidate generation is not the same as semantic judgment.

Useful generic candidate harvesters:

```text
IDR amount tokens
relative-date tokens
explicit date tokens
time tokens
known account aliases
known merchant aliases
category slugs
opaque transaction refs
currently allowed workflow actions
```

Rules:

- harvesters MUST be generic, not bank-template specific;
- harvesting must not silently choose a candidate when multiple exist;
- Jev may choose among candidates;
- if no candidate exists for a required arbitrary value, use the generative extractor or clarify.

This pattern minimizes both LLM calls and hand-written semantic logic.

---

# 22. Richmod System One Client

Add a provider-neutral client package, for example:

```text
apps/worker/internal/judgment
```

Suggested interface:

```go
type Request struct {
    State     any
    Questions map[string]Question
}

type Result struct {
    Model   string
    Answers map[string]Answer
    Usage   Usage
}

type Engine interface {
    Evaluate(ctx context.Context, requestID string, req Request) (Result, error)
}
```

Gateway implementation:

```text
apps/worker/internal/judgment/systemone
```

It talks only to:

```text
<LLM_GATEWAY_BASE_URL>/systemone
```

Given the existing deployment default:

```text
LLM_GATEWAY_BASE_URL=http://9router:20128/v1
```

the effective endpoint is:

```text
http://9router:20128/v1/systemone
```

The System One client reuses the existing LiteRouter client API credential:

```text
LLM_GATEWAY_API_KEY
```

It does not have a TypeSafe provider key.

Suggested additional configuration:

```text
JUDGMENT_MODEL=typesafe/jev-latest
JUDGMENT_TIMEOUT_MS=3000

JUDGMENT_CHOICE_MIN_PROBABILITY=0.85
JUDGMENT_CHOICE_MIN_MARGIN=0.20
JUDGMENT_NOUL_HIGH=0.90
JUDGMENT_NOUL_LOW=0.10
```

Per-workflow thresholds may override the defaults when justified.

---

# 23. Native System One Contract

Richmod sends the native request shape through LiteRouter:

```json
{
  "model": "typesafe/jev-latest",
  "state": {
    "source": {},
    "server_context": {},
    "candidates": {}
  },
  "questions": {
    "route": {
      "type": "choice",
      "instructions": "Choose the workflow that best matches the user request.",
      "criteria": []
    }
  }
}
```

Exact primitive fields MUST follow the deployed System One/TypeSafe API contract at implementation time.

Richmod MUST:

- validate expected question keys;
- validate answer type;
- validate probabilities;
- reject missing expected answers;
- reject unexpected model-level schema changes;
- cap response size;
- use context cancellation;
- record model/gateway metadata without raw financial source content.

Richmod does not need native-tool-call parsing for System One decisions.

---

# 24. Failure Policy

Jev is a production decision dependency for workflows that choose to use it.

Failure does not mean "trust the LLM instead".

For a Jev-owned bounded decision:

```text
timeout
gateway failure
rate limit
upstream overload
malformed response
missing answer
invalid probability
unsupported model
```

results in one of:

```text
deterministic fallback if the exact answer can still be computed
clarification
Review Inbox
retryable background job
generative agent only if the task genuinely changes into an unbounded extraction/reasoning task
```

Do not silently substitute a generative model to answer the same Jev question.

This preserves semantic consistency.

---

# 25. Reducing Native Tool Complexity

A successful implementation should shrink the generative tool surface.

Current examples that may eventually disappear from the conversational LLM catalog when fully Jev-owned:

```text
resolve_salary_choice
resolve_merchant_learning
bounded resolve_review actions
pending batch decision
finance_out_of_scope
simple finance route selection
```

`record_transaction` and `record_transfer` may remain available as generative fallback tools for turns requiring arbitrary extraction, but should no longer be the mandatory path for simple explicit commands.

Generative `confidence` and `category_confidence` should become non-authoritative immediately and should be removed from schemas once downstream compatibility no longer needs them.

The target is fewer prompts, fewer generated JSON arguments, fewer schema retries, and fewer duplicated confidence fields.

---

# 26. Observability

Add provider-neutral decision telemetry.

Recommended fields:

```text
task
protocol = systemone
model
status
error_class
duration_ms
question_count
question_keys
policy_version
outcome
used_generative_fallback
source_event_id
household_id
created_at
```

Do not log:

- raw Telegram text;
- raw email body;
- document contents;
- System One provider key;
- full model request/response bodies.

Measure whether Jev is actually reducing generative work:

```text
telegram_turns_total
telegram_turns_jev_only
telegram_turns_generative
telegram_turns_jev_then_generative
native_tool_calls_avoided
review_rate
clarification_rate
judgment_latency
generative_latency
```

These metrics are essential to prove the integration has product value.

---

# 27. Auditability

For canonical changes influenced by Jev, audit should be able to reconstruct:

```text
source event
server-owned candidates
question keys
selected answer/probability
policy threshold/version
deterministic validation outcome
canonical mutation/review outcome
```

Do not store raw Jev request state merely for convenience.

The audit authority is the Go decision that consumed Jev signals.

---

# 28. Security and Privacy

All model inference remains behind LiteRouter.

Richmod:

- sends its LiteRouter API key only to LiteRouter;
- never stores TypeSafe credentials;
- minimizes System One state;
- never sends SQL or DB credentials;
- never lets model text choose canonical IDs;
- never sends unrelated household history;
- preserves household scoping before model invocation;
- treats user/email/document text as untrusted data.

LiteRouter owns TypeSafe provider credential storage and provider fallback.

---

# 29. Direct Production Rollout

There is no shadow-mode requirement.

Roll out by capability:

## Phase A — System One client foundation

- `internal/judgment/systemone`;
- LiteRouter `/v1/systemone`;
- strict decode/telemetry;
- configuration;
- tests.

## Phase B — bounded server-state interactions

Replace generative tool calls for:

- salary choice;
- merchant-learning reply;
- pending batch decision;
- bounded review resolution.

These have the clearest typed state and lowest migration risk.

## Phase C — category and transfer semantics

- Jev category Choice;
- Jev transfer-purpose Choice;
- remove generative confidence from authority;
- route uncertainty to review.

## Phase D — Telegram Jev-first fast path

- route Choice;
- generic candidate harvesting;
- simple READs without generative agent;
- simple transaction creation without generative agent;
- generative fallback only when arbitrary extraction/reasoning is needed.

## Phase E — email/reconciliation

- evidence support judgments;
- unresolved transfer semantic classification;
- semantic reconciliation after deterministic filters.

## Phase F — analytics/document optimization

- Jev structured insight selection;
- migrate document bounded confidence/classification when sufficient text-backed evidence exists.

---

# 30. Testing Requirements

## System One client

Test:

- request encoding;
- LiteRouter auth;
- model field;
- Noul response;
- Choice response;
- Score response if introduced;
- missing answers;
- unexpected question;
- invalid probability;
- timeout;
- 401;
- 429;
- 529/upstream overload;
- response-size cap;
- no direct TypeSafe credential.

## Telegram bounded workflows

Test:

- review CONFIRM/IGNORE/classification action;
- salary PRIMARY/ORDINARY/IGNORE;
- merchant learning yes/no/ambiguous;
- batch confirm/cancel/update/defer;
- exact callback bypasses Jev when deterministic;
- stale server binding fails regardless of Jev.

## Jev-first route

Test:

- "berapa pengeluaran bulan ini" -> Jev route + deterministic READ, no generative call;
- "saldo wealth sekarang" -> Jev route + deterministic READ, no generative call;
- "catat bensin 50rb" -> candidate harvest + Jev + Go, no generative call;
- ambiguous amount -> generative extraction or clarification;
- open-ended analytics question -> generative agent;
- non-finance -> bounded Jev classification.

## Category / transfer

Test:

- strong category winner;
- low probability;
- insufficient margin;
- OTHER_OR_UNCLEAR;
- exact merchant rule bypasses Jev;
- explicit user category bypasses Jev;
- transfer known-own-account deterministic;
- unresolved transfer purpose via Jev;
- Jev cannot bypass invalid Go account/reconciliation rule.

## Bank email

Test:

- arbitrary extraction still generative;
- Jev verifies extracted semantic facts;
- deterministic reconciliation precedes Jev;
- unclear transfer semantics routes by Jev;
- provider failure does not trust LLM confidence.

---

# 31. Acceptance Criteria

The integration is complete when:

1. All external AI inference still goes through LiteRouter.
2. Richmod uses `/v1/systemone` natively rather than translating Jev into OpenAI Responses.
3. Richmod never needs the upstream TypeSafe key.
4. Jev replaces generative native-tool calls for bounded server-state decisions.
5. Simple Telegram READ requests can complete without a generative model.
6. Common explicit transaction commands can complete without a generative model when candidate harvesting is sufficient.
7. Category classification is Jev-owned after deterministic rules.
8. Unresolved transfer semantics can use Jev after deterministic account/reconciliation logic.
9. Generative self-reported confidence is not authoritative on Jev-enabled workflows.
10. Generative LLM remains available for arbitrary extraction, vision, reasoning, and prose.
11. Go remains the only authority for canonical mutation.
12. Exact server bindings cannot be overridden by any model.
13. Jev failure never silently becomes generative decision fallback for the same bounded question.
14. Review/clarification receives ambiguous cases.
15. Telemetry can quantify generative calls avoided.
16. ADR-038, ADR-005, README, and AGENTS.md describe the new architecture.
17. No secret is committed.
18. Relevant tests pass before implementation is merged.

---

# 32. Final Architecture

```text
                           RICHMOD

                      user / evidence
                            |
                            v
                 Go deterministic context
              validation + candidate harvesting
                            |
                            v
                  bounded decision needed?
                     /              \
                   no                yes
                   |                  |
                   |                  v
                   |          LiteRouter /systemone
                   |                  |
                   |                 Jev
                   |                  |
                   |          typed probabilities
                   |                  |
                   |          enough to finish?
                   |             /        \
                   |           yes         no
                   |            |           |
                   |            |           v
                   |            |   LiteRouter /responses
                   |            |           |
                   |            |    generative LLM
                   |            |  extraction/reasoning
                   |            |           |
                   +------------+-----------+
                                |
                                v
                           Go policy
                    auth / bind / reconcile
                         /            \
                    canonical         Review
                     mutation          Inbox
                         |
                         v
                     PostgreSQL
```

> Use deterministic code for facts, Jev for bounded judgment, generative models for open-ended intelligence, and Go for authority.
