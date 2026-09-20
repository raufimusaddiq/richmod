# RICHMOD JEV / SYSTEM ONE INTEGRATION PRD

**Status:** Ready for implementation — direct production integration  
**Repository:** `raufimusaddiq/richmod`  
**Target branch:** `RICHMOD`  
**Implementation branch:** `docs/jev-system-one-prd`  
**Baseline:** `main@81e466ca0f71c8c056dd2c500f4ca17fb4466c51`  
**Date:** 2026-09-20  

---

## 1. Product Goal

Integrate TypeSafe AI Jev into Richmod as a first-class semantic judgment layer.

Responsibility split:

```text
Generative LLM = understand, extract, converse, propose
Jev            = typed semantic judgments with probabilities
Go             = authorize, validate, reconcile, review, commit
PostgreSQL     = canonical truth
```

Jev is **not** a replacement for the Telegram conversational model, document vision model, or insight writer.

This rollout is **not shadow mode**. Once enabled, covered production paths actively depend on Jev judgments.

---

## 2. Existing Richmod Invariants

Implementation MUST preserve:

- PostgreSQL remains canonical financial state.
- Go owns every financial state transition.
- Generative-model output remains untrusted.
- Jev output is also untrusted input to Go policy, never mutation authority.
- Neither model receives database credentials or direct DB access.
- Exact Telegram callback / `reply_to_message_id` binding remains authoritative.
- Existing reconciliation semantics remain authoritative.
- Evidence remains preserved and auditable.
- Deterministic READ and calculation paths continue without Jev.
- No Python service or sidecar is introduced.

Because this changes the AI architecture, implementation MUST add a dedicated ADR before merge to `main`.

---

## 3. Problem

Richmod currently allows a generative model to both:

1. propose a semantic interpretation; and
2. emit confidence about its own proposal.

Example:

```text
User:
"beli bensin 50rb tadi"

Generative proposal:
type = EXPENSE
amount = 50000
category = transport
confidence = 0.94
category_confidence = 0.91
```

The same model is deciding the answer and grading itself.

Target architecture:

```text
unstructured source
      |
      v
generative model
      |
      v
typed proposal
      |
      v
Go structural validation
      |
      v
Jev semantic judgments
      |
      v
Go policy
   /      \
commit   review
```

Generative models stay responsible for language and extraction. Jev becomes useful for bounded classification, verification, and confidence-aware branching.

---

## 4. Why Jev Fits Richmod

Jev is designed around:

```text
state + typed questions -> typed probabilistic answers
```

rather than:

```text
messages -> generated string
```

That matches Richmod's existing rule that AI can interpret, while Go owns canonical financial state.

P0 should use:

- **Noul** for yes/no probabilistic predicates.
- **Choice** for mutually competing known alternatives.

P0 does not require Score.

References current at PRD creation time:

- https://typesafe.ai/blog/introducing-system-one-models-and-jev
- https://typesafe.ai/
- https://learnjev.com/reference
- https://learnjev.com/tutorials/three-primitives

API details MUST be rechecked against current TypeSafe documentation during implementation because Jev is new and may evolve quickly.

---

## 5. Explicit Non-Goals

Jev MUST NOT replace:

```text
Telegram natural conversation
final user-facing prose
multi-phase conversational reasoning
document image understanding
OCR / arbitrary visual extraction
financial insight prose
transaction search
READ-tool orchestration
```

Jev MUST NOT:

```text
run SQL
receive database credentials
choose hidden canonical database IDs
bypass household authorization
bypass exact reply/callback binding
bypass reconciliation rules
commit a transaction directly
resolve a review directly
```

Do not turn Jev into a second autonomous agent framework.

---

# 6. P0 — Telegram Side-Effect Semantic Gate

Every **generative-model-originated Telegram side effect** must pass Jev before Go executes it.

Initial covered actions include:

```text
record_transaction
record_transaction_batch
record_transfer
propose_transaction_correction
resolve_review
resolve_salary_choice
resolve_merchant_learning
wealth-related side effects
```

Deterministic callbacks or exact button actions do not require Jev when Go already has an exact target, explicit transition, and authenticated actor.

Target flow:

```text
user text
   |
   v
conversational LLM
   |
   v
one SIDE EFFECT proposal
   |
   v
Go schema/domain validation
   |
   v
Jev semantic gate
   |
   v
Go authorization / binding / reconciliation
   |
   +--> execute
   |
   +--> clarify/review
```

### Critical rule

Jev may judge **what the user appears to mean**.

Jev may not decide **which hidden canonical row to mutate**.

ADR-033 server-owned binding continues to take precedence.

---

## 7. Telegram Judgment Questions

Avoid one opaque question such as:

```text
safe_to_commit?
```

Use decomposed judgments.

For transaction creation:

```text
transaction_intent_supported
amount_supported
type_supported
category
material_ambiguity
```

For transfer:

```text
transfer_intent_supported
amount_supported
source_account_semantics_supported
destination_semantics_supported
purpose_supported
material_ambiguity
```

For corrections:

```text
correction_intent_supported
requested_change_supported
material_ambiguity
```

For review resolution:

```text
resolution_intent_supported
selected_resolution_semantics
material_ambiguity
```

Exact target binding remains Go-owned.

---

# 8. P0 — Category Judgment

Generative `category_confidence` must stop being final auto-confirm authority on Jev-enabled paths.

The generative model may propose a `category_slug`, but Jev independently evaluates category fit.

Use a Jev **Choice** over active Richmod categories and always include:

```text
OTHER_OR_UNCLEAR
```

Suggested initial policy:

```text
winner != OTHER_OR_UNCLEAR
AND top_probability >= 0.85
AND top_probability - second_probability >= 0.20
```

These are Richmod configuration defaults, not universal claims about Jev.

If policy fails:

```text
-> existing category review
```

Do not force probability mass onto a normal category when evidence is weak.

---

# 9. P0 — Bank Email Semantic Verification

Keep the current bank-email native LLM extraction.

Target flow:

```text
trusted inbound email
   |
   v
native LLM extraction
   |
   v
Go structural validation
   |
   v
Jev evidence-vs-proposal judgment
   |
   v
Go policy / proposal / review
```

Initial predicates:

```text
transaction_observed
amount_supported
direction_supported
channel_supported
merchant_or_counterparty_supported
material_ambiguity
```

Jev is not asked to re-extract arbitrary strings.

Example:

```text
LLM extracted:
amount_idr = 500000
direction = OUT
channel = TRANSFER

Jev judges:
Does the source support amount 500000?
Does it support OUT?
Does it support TRANSFER?
```

Jev must not decide canonical accounting semantics such as:

```text
EXPENSE
OWN_ACCOUNT_TRANSFER
HOUSEHOLD_TRANSFER
INVESTMENT_TRANSFER
ASSET_PURCHASE
```

Those remain owned by existing Go policy, reconciliation, and review workflows.

---

# 10. P1 — Review Inbox Explainability

Jev may contribute structured review reasons.

Suggested normalized reasons:

```text
AMOUNT_UNSUPPORTED
TYPE_AMBIGUOUS
CATEGORY_AMBIGUOUS
ACTION_INTENT_AMBIGUOUS
EVIDENCE_INSUFFICIENT
COUNTERPARTY_AMBIGUOUS
TRANSFER_SEMANTICS_AMBIGUOUS
```

Review UI may render:

```text
✓ Amount supported
✓ Expense intent supported
! Category ambiguous between Groceries and Household
```

Do not expose raw provider payloads.

---

# 11. Deferred — Document Integration

Jev is not the primary vision model.

The existing document path stays:

```text
image / PDF
   |
   v
generative vision extraction
   |
   v
Go validation
```

Do not ask Jev to verify information that only exists visually unless Richmod first has a text/structured representation Jev can actually evaluate.

Document integration is not required for P0.

---

# 12. Jev Question-Type Rules

## Noul

Use for predicates whose probability maps directly to policy:

```text
amount_supported
action_matches_user_intent
transaction_observed
material_ambiguity
evidence_sufficient
```

A Noul around `0.5` means uncertainty, not a medium score.

## Choice

Use for competing known alternatives:

```text
category
review_reason
semantic_family
```

Include an escape option when the set may be incomplete.

## Score

Do not use in P0 unless a concrete ordered qualitative decision requires it.

---

# 13. Jev State Design

Go constructs provider state. It is application state, not a conversational prompt.

Example:

```json
{
  "source": {
    "type": "TELEGRAM_TEXT",
    "user_text": "tadi beli bensin 50 ribu"
  },
  "proposal": {
    "tool": "record_transaction",
    "type": "EXPENSE",
    "amount_idr": "50000",
    "category_slug": "transport",
    "merchant": null,
    "date_reference": "TODAY"
  },
  "policy_context": {
    "currency": "IDR",
    "allowed_categories": [
      "food_dining",
      "groceries",
      "transport",
      "household",
      "health",
      "other"
    ]
  }
}
```

Send only data required for selected questions.

Never send:

```text
database credentials
SQL
session secrets
provider credentials
unrelated household history
unnecessary canonical UUIDs
raw attachments Jev cannot process
```

---

# 14. Go Architecture

Add a provider-neutral package:

```text
apps/worker/internal/judgment
```

Suggested contract:

```go
type Request struct {
    State     any
    Questions map[string]Question
}

type Result struct {
    Provider string
    Model    string
    Answers  map[string]Answer
    Usage    Usage
}

type Engine interface {
    Evaluate(ctx context.Context, requestID string, req Request) (Result, error)
}
```

TypeSafe implementation:

```text
apps/worker/internal/judgment/typesafe
```

Domain packages must not depend on raw TypeSafe response structs.

Use a narrow Go HTTP client. Do not add Python or Node sidecars.

Expected provider routes at design time:

```text
POST /v1/systemone
GET  /v1/models
```

Provider host and model are configuration, not financial business logic.

---

# 15. Configuration

Suggested configuration:

```text
JEV_ENABLED=true
JEV_BASE_URL=https://api.typesafe.ai
JEV_MODEL=jev-latest
JEV_TIMEOUT_MS=3000

JEV_TRANSACTION_EVIDENCE_THRESHOLD=0.90
JEV_AMOUNT_THRESHOLD=0.95
JEV_ACTION_INTENT_THRESHOLD=0.90
JEV_MAX_AMBIGUITY=0.10

JEV_CATEGORY_MIN_PROBABILITY=0.85
JEV_CATEGORY_MIN_MARGIN=0.20
```

Provider authentication must be supplied only through deployment secrets/environment.

No real credential belongs in repository files, docs, tests, fixtures, PR text, CI logs, or screenshots.

Thresholds belong to Richmod policy, not hidden inside model instructions.

---

# 16. Decision Policy

## Telegram

Execute a generative side effect only when:

```text
Go structural validation passes
AND Go authorization passes
AND server-owned binding passes
AND action_matches_user_intent >= configured threshold
AND material_ambiguity <= configured maximum
AND operation-specific Jev judgments pass
AND existing reconciliation/domain policy passes
```

A high Jev probability cannot rescue invalid money, unauthorized operations, stale targets, invalid transitions, or failed reconciliation invariants.

## Category

Accept only when:

```text
winner != OTHER_OR_UNCLEAR
AND top probability >= configured minimum
AND margin to second option >= configured minimum
```

Otherwise route to category review.

## Bank Email

Jev verifies proposed observed facts. It cannot override deterministic validation.

---

# 17. Failure Policy

This is direct production integration.

For Jev-required mutation paths, any of these:

```text
timeout
network failure
authentication failure
rate limit
provider overload
malformed JSON
missing answer
unexpected answer type
invalid probability
```

must result in:

```text
NO AI-originated canonical mutation
```

Then use appropriate existing behavior:

```text
review
clarification
deterministic retry/failure response
safe job retry
```

There is **no fallback** to generative self-reported confidence.

Existing deterministic features continue normally when Jev is unavailable.

---

# 18. Telegram Agent After Jev

ADR-033 remains the conversational model.

```text
model phase
  |
  +--> final text
  |
  +--> READ batch -> Go -> next model phase
  |
  +--> SIDE EFFECT
          |
          v
       Go validate
          |
          v
         Jev
          |
          v
       Go policy
        /    \
     execute review/clarify
```

Do not invoke Jev for normal READ batches.

Post-mutation user-facing synthesis remains generative.

---

# 19. Provider Client Requirements

The Go client must:

- use bearer authentication;
- use JSON;
- use configurable base URL;
- use configured model;
- propagate context cancellation;
- enforce a bounded timeout;
- cap response size;
- strictly decode responses;
- allow-list requested answer IDs;
- validate answer types;
- validate probabilities are within `0..1`;
- classify authentication, validation, rate-limit, overload, transport, and invalid-response errors;
- never execute financial actions;
- never receive database credentials.

One Jev request should contain all independent questions for the same decision state where practical.

---

# 20. Observability and Audit

Add provider-neutral judgment telemetry.

Recommended metadata:

```text
task
provider
model
status
error_class
duration_ms
question_count
question_keys
policy_version
outcome
household_id
source_event_id
created_at
```

Do not store raw Telegram text, email body, document contents, or full provider payloads in operational metrics.

For every canonical mutation gated by Jev, Richmod must be able to reconstruct:

```text
source event
generative action proposal
relevant Jev judgments
policy version
deterministic validation result
final Go outcome
```

The audit subject is the final Go decision.

---

# 21. Optional Data Model

Do not add another financial ledger.

If existing audit tables cannot cleanly represent judgment provenance, add an audit-style record such as:

```text
ai_judgment
-----------
id
household_id
source_event_id
task
provider
model
policy_version
status
outcome
answers_jsonb
duration_ms
created_at
```

`answers_jsonb` contains bounded typed answers only, never raw source content.

Any schema change requires migration, integration tests, `docs/DATABASE_SCHEMA.md` update, and ERD update.

---

# 22. Product Examples

## Clear expense

```text
User:
"beli bensin 50rb tadi"

LLM:
EXPENSE / 50000 / transport

Jev:
transaction intent -> high
amount support      -> high
expense support     -> high
category transport  -> dominant
ambiguity           -> low

Go:
record transaction
```

## Ambiguous exact amount

```text
User:
"makan tadi sekitar 50an"

LLM:
amount = 50000

Jev:
transaction intent -> high
exact amount        -> weak
ambiguity           -> high

Go:
ask clarification or review
```

## Ambiguous category

```text
User:
"belanja di Superindo 180 ribu"

Jev Choice:
groceries          0.57
household          0.34
OTHER_OR_UNCLEAR   0.09

Go:
category review
```

## Exact review callback

```text
User clicks CONFIRM on a server-bound review.

Go already owns exact target and transition.

Jev:
not required
```

## Provider failure

```text
LLM proposes a transaction.
Jev times out.

Go:
does not execute proposal;
does not trust LLM confidence;
uses deterministic review/retry behavior.
```

---

# 23. Implementation Sprints

## Sprint A — Foundation

- add ADR for System One/Jev boundary;
- add `internal/judgment`;
- add TypeSafe Go HTTP adapter;
- add configuration;
- add strict response validation;
- add telemetry;
- add unit tests.

## Sprint B — Telegram Production Gate

- gate conversational SIDE EFFECT calls;
- implement intent, amount, ambiguity, and category questions;
- preserve exact binding;
- add integration tests;
- enable production path.

## Sprint C — Bank Email

- construct minimized source/proposal state;
- add evidence-support judgments;
- connect outcomes to current review policy;
- preserve current extractor contract;
- add integration tests.

## Sprint D — Review Explainability

- normalize Jev-backed review reasons;
- expose understandable review explanation;
- preserve typed decision provenance.

## Sprint E — Optional Expansion

- financial-provider email;
- additional bounded classifiers;
- document judgment only when text-backed state exists.

---

# 24. Required Tests

Unit tests:

```text
request encoding
response decoding
Noul
Choice
timeout
authentication failure
rate limit
overload
malformed JSON
missing answer
wrong answer type
out-of-range probability
category threshold
category margin
no generative-confidence fallback
```

Telegram integration tests:

```text
clear expense
clear income
transfer
ambiguous amount
ambiguous category
correction
exact reply-bound review
multiple reviews
stale review
Jev timeout
malformed Jev response
Jev cannot override invalid Go rule
READ-only turn does not invoke Jev
```

Bank email tests:

```text
supported amount
unsupported amount
transfer observation
ambiguous counterparty
Jev provider failure
deterministic invalid input remains invalid
```

---

# 25. Acceptance Criteria

Complete when:

1. Jev is an active production dependency for covered generative Telegram side effects.
2. No shadow-only launch mode is required.
3. The conversational model still owns natural-language conversation.
4. READ-only flows do not unnecessarily call Jev.
5. Jev cannot mutate PostgreSQL.
6. Go owns every final financial decision.
7. Generative self-confidence is no longer final category/auto-confirm authority on Jev-enabled paths.
8. Category Choice includes `OTHER_OR_UNCLEAR`.
9. Exact server bindings cannot be overridden by Jev.
10. Provider failure never silently means "trust the generative model".
11. Deterministic Richmod features remain functional without Jev.
12. No real provider credential is committed or logged.
13. Relevant unit and integration tests pass.
14. ADR and architecture documentation are updated.
15. Production enablement is configuration-only.

---

# 26. Final Architecture

```text
                         RICHMOD

Telegram ---------+
Bank email -------+--> Generative AI --> typed proposal
Documents --------+                         |
                                             v
                                   Go structural validation
                                             |
                          +------------------+------------------+
                          |                                     |
                  deterministic path                  semantic judgment
                          |                                     |
                          |                                     v
                          |                                    Jev
                          |                             typed probabilities
                          |                                     |
                          +------------------+------------------+
                                             v
                                         Go policy
                                      /             \
                                  canonical         Review
                                   mutation          Inbox
                                      |
                                      v
                                  PostgreSQL
```

> Generative AI may understand and propose. Jev may judge. Go authorizes and decides. PostgreSQL remains truth.
