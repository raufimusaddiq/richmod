# RICHMOD JEV COMPLETION PRD

**Status:** Remaining work after PR #91 and PR #93  
**Repository:** `raufimusaddiq/richmod`  
**Baseline:** `main@593855a17e9f2fc6791c1593c2d47cd96c8a170a`  
**Date:** 2026-09-22  
**Scope:** Gap-only. Do not reimplement work already landed.

---

# 1. Executive Summary

Richmod now has a real Jev/System One vertical slice and several important bounded workflows already migrated.

PR #91 landed:

- LiteRouter-native `/v1/systemone` client;
- judgment abstraction;
- bounded response handling;
- configuration and telemetry;
- initial Jev-first Telegram READ routing.

PR #93 then landed:

- pending correction confirm/cancel through Jev;
- pending batch confirm/cancel/defer through Jev, with UPDATE falling through to generative extraction;
- salary choice through Jev;
- merchant-learning consent through Jev;
- fact-free bound review actions through Jev;
- simple explicit transaction commands with no generative call when one amount can be harvested;
- Jev category classification after deterministic merchant rules.

The original architecture is therefore partially real:

```text
Go exact state
   |
   v
Jev bounded judgment
   |
   +--> Go can finish
   |
   +--> generative LLM only when arbitrary values/reasoning are needed
```

The remaining work has changed. The next sprint is no longer primarily about adding Jev to Telegram. It must:

1. align Richmod with the real native System One contract;
2. fix READ-period correctness;
3. turn Telegram judgment into one parallel decision bundle instead of multiple serial Jev calls;
4. remove remaining generative self-confidence authority;
5. move transfer/email/reconciliation/insight decisions that are genuinely bounded to Jev;
6. add telemetry/audit that proves Jev is actually reducing generative work.

---

# 2. Latest-Main Audit

Latest main:

```text
593855a17e9f2fc6791c1593c2d47cd96c8a170a
Merge PR #93
feat: complete Jev System One bounded workflows and simple commands
```

## Already implemented — preserve these

### Gateway boundary

```text
Richmod -> LiteRouter /v1/systemone -> TypeSafe
Richmod -> LiteRouter /v1/responses -> generative models
```

Richmod does not hold the upstream TypeSafe credential.

### Telegram Jev route layer

Jev currently selects bounded route families before the generative agent.

### Bounded server-state workflows

Already Jev-first:

```text
pending correction confirm/cancel
pending batch confirm/cancel/defer
salary PRIMARY/ORDINARY/IGNORE
merchant-learning remember/do-not-remember
fact-free review actions
```

### Simple transaction fast path

For sufficiently simple text, Go harvests an amount/date candidate, Jev decides transaction type/category, and Go persists without a generative agent call.

### Category classification

Expense category selection now uses:

```text
confirmed merchant rule
   -> deterministic category

otherwise
   -> Jev Choice
```

These are no longer gaps and must not be reimplemented.

---

# 3. P0 — Native System One Request Contract

Current Richmod still models every question as:

```go
type Question struct {
    Type         string
    Instructions string
    Criteria     []string
}
```

This is too weak for the native System One contract.

The three primitives have different criteria shapes:

```text
Noul
  yes/no probability
  optional true/false criteria

Choice
  one option from a criteria map
  probabilities over every option
  confidence

Score
  ordered levels
  continuous score
  probabilities over levels
  confidence
```

## Required design

Introduce primitive-specific request types or a tagged union that preserves these distinctions.

Suggested logical API:

```go
type NoulCriteria struct {
    True  any
    False any
}

type NoulQuestion struct {
    Instructions any
    Criteria     *NoulCriteria
}

type ChoiceQuestion struct {
    Instructions any
    Criteria     map[string]any
}

type ScoreQuestion struct {
    Instructions any
    Criteria     []any
}
```

The implementation may use interfaces/custom marshalers, but invalid combinations must fail locally.

---

# 4. P0 — Route Choice Must Carry Criteria

Latest `tryJudgmentFastPath` places routes in:

```text
state.allowed_routes
```

but creates the `route` Choice without Choice criteria.

That is not sufficient for a native Choice.

Target:

```json
{
  "state": {
    "user_text": "..."
  },
  "questions": {
    "route": {
      "type": "choice",
      "instructions": "Which Richmod workflow best matches this request?",
      "criteria": {
        "READ_SPENDING": "Expense totals or breakdowns",
        "READ_CASHFLOW": "Income/outflow/net cashflow",
        "CREATE_TRANSACTION": "Record one income or expense",
        "OTHER_OR_UNCLEAR": "No safe route"
      }
    }
  }
}
```

Choice labels/descriptions are part of the model input. Do not substitute a list hidden only in state.

The same change applies to:

- bounded workflow choices;
- salary choice;
- category choice;
- transaction type;
- review action;
- future transfer purpose.

---

# 5. P0 — Native Answer Decoding

Current decoder expects generic fields such as:

```text
probability
distribution
value
score
```

Decode each primitive using its native semantics instead.

## Noul

Logical shape:

```json
{
  "type": "noul",
  "noul": 0.92
}
```

A Noul is the yes-probability. There is no separate confidence field.

## Choice

Logical shape:

```json
{
  "type": "choice",
  "choice": "READ_SPENDING",
  "probabilities": {
    "READ_SPENDING": 0.91,
    "READ_CASHFLOW": 0.06,
    "OTHER_OR_UNCLEAR": 0.03
  },
  "confidence": 0.88
}
```

Retain:

```text
choice
probabilities
confidence
```

## Score

Retain:

```text
score
legend
probabilities
confidence
```

Do not assume Score is restricted to 0..1. Its semantic range depends on the ordered rubric.

---

# 6. P0 — Strict Choice Acceptance

Current `AcceptChoice` can accept a Choice when no full distribution is present.

Automatic decisions must require:

```text
selected option exists in server criteria
probabilities contain every expected option
no unexpected option is present
all probabilities are finite and in [0,1]
probabilities approximately sum to 1
selected option is an argmax
top probability >= workflow minimum
top - second >= workflow margin
confidence is valid
confidence >= workflow minimum when configured
```

Do not synthesize a missing distribution.

---

# 7. P0 — Real LiteRouter → TypeSafe Smoke

Current unit tests prove Richmod transport behavior against mocked HTTP responses.

Add an opt-in real integration smoke that validates:

```text
Richmod
  -> LiteRouter /v1/systemone
  -> TypeSafe
  -> LiteRouter pass-through
  -> Richmod decoder
```

Minimum assertions:

- real provider accepts the native question schema;
- LiteRouter normalizes/routs the model correctly;
- Richmod decodes the actual provider response;
- actual versioned model ID is captured;
- only the LiteRouter client key exists in Richmod;
- no TypeSafe provider key enters Richmod.

This smoke should run only in an approved secret-bearing environment.

---

# 8. P0 Correctness — READ Period Is Still Hardcoded

Latest `tryJudgmentFastPath` still resolves `THIS_MONTH` for spending, cashflow, and savings.

Therefore:

```text
"pengeluaran hari ini"
"cashflow minggu ini"
"tabungan bulan lalu"
```

can route to the right READ family but execute with the wrong time range.

## Required fix

Ask route and period in the same System One request.

Period Choice:

```text
TODAY
THIS_WEEK
LAST_WEEK
THIS_MONTH
LAST_MONTH
CURRENT_CYCLE
PREVIOUS_CYCLE
CUSTOM_OR_UNCLEAR
```

Go ignores period for routes that do not need it.

For `CUSTOM_OR_UNCLEAR`:

- use deterministic explicit-date candidates if available; otherwise
- fall through to extraction/clarification.

Never silently substitute THIS_MONTH.

---

# 9. P0 — One Parallel Telegram Decision Bundle

Current common path may call Jev once for route and again for transaction type/category.

System One is explicitly designed to answer multiple independent questions in parallel over the same state.

Preferred architecture:

```text
Telegram text
    |
    v
Go server context + candidate harvesting
    |
    v
ONE System One request
    |
    +--> route Choice
    +--> period Choice
    +--> transaction_type Choice
    +--> category Choice
    +--> amount_supported Noul
    +--> date_supported Noul
    +--> material_ambiguity Noul
    |
    v
Go consumes only answers relevant to selected route
```

Speculative questions are acceptable when they eliminate a second network round-trip and remain cheap.

Do not turn Jev into a serial pseudo-agent.

---

# 10. P0 — Dynamic Criteria From Server State

Server state determines what Jev is allowed to choose.

Examples:

## Ordinary free text

Expose relevant route options plus `OTHER_OR_UNCLEAR`.

## Pending salary

Skip global routing and directly ask:

```text
PRIMARY
ORDINARY
IGNORE
OTHER_OR_UNCLEAR
```

## Exact review binding

Expose only legal actions for that review type.

## Pending batch

Expose only legal batch transitions.

The server defines the possibility space; Jev performs semantic judgment inside it.

---

# 11. P1 — Merchant Learning Should Use Noul

PR #93 already removed the generative tool call for normal merchant-learning replies.

It currently models consent as a Choice:

```text
REMEMBER
DO_NOT_REMEMBER
OTHER_OR_UNCLEAR
```

Explicit consent is naturally a Noul:

```text
did_user_explicitly_consent_to_remember_this_merchant_category_rule?
```

Policy example:

```text
noul >= high threshold -> remember
noul <= low threshold  -> do not remember
otherwise              -> clarification
```

This is a cleanup, not a blocker for PR #93 behavior.

---

# 12. P1 — Complete the Simple Transaction Bundle

PR #93 created a real zero-generative simple transaction path.

Current Jev responsibility is mainly:

```text
transaction type
category
```

Expand the same call with explicit support/ambiguity questions:

```text
is_record_transaction_request?      Noul
transaction_type                    Choice
amount_candidate_supported?         Noul
date_candidate_supported?           Noul
category                            Choice
material_ambiguity?                 Noul
```

Then Go composes policy.

Do not treat transaction-type Choice probability as an overall transaction confidence.

---

# 13. P1 — Remove Generative Confidence From Mutation Authority

Latest main still keeps:

```text
confidence
category_confidence
ambiguous
```

in generative extraction.

The simple Jev path currently reuses legacy fields such as `Confidence` to carry a Jev probability. The generative fallback path still has model-generated overall confidence.

Final target:

```text
Go structural validation
AND Jev intent support
AND Jev amount support
AND Jev date support when relevant
AND category policy
AND low ambiguity
AND deterministic domain rules
```

No opaque generative:

```text
confidence >= 0.90
```

should authorize auto-confirm.

Migration:

1. make generative confidence non-authoritative;
2. keep temporarily for telemetry/backward compatibility;
3. migrate decisions to explicit Jev questions;
4. remove confidence fields from generative schemas after consumers are gone.

---

# 14. P1 — Finish Category Ownership

PR #93 correctly moved category selection behind deterministic merchant rules.

Remaining:

- native Choice criteria map;
- actual Choice probabilities/confidence in provenance;
- explicit user category detection as deterministic input where possible;
- weak category result routes through the normal Review Inbox semantics;
- remove category authority from generative tool arguments after migration.

`category_confidence` should eventually disappear as an application decision field.

---

# 15. P1 — Transfer Purpose Still Belongs to Jev

`record_transfer` still receives a bounded `purpose` enum from the generative model.

Target purpose Choice:

```text
SAVINGS_TRANSFER
INVESTMENT_CONTRIBUTION
ASSET_PURCHASE
DEBT_PRINCIPAL_PAYMENT
INTERNAL_TRANSFER
OTHER_OR_UNCLEAR
```

Order:

```text
exact deterministic account/reconciliation rule
        |
        +--> decisive -> Go
        |
        +--> unresolved -> Jev Choice
```

Use the generative model only when arbitrary account/description extraction is required.

---

# 16. P1 — Preserve the Review Split From PR #93

PR #93 correctly handles fact-free review actions with Jev and leaves arbitrary-value actions to the generative path.

Preserve this boundary.

Jev-owned examples:

```text
CONFIRM
IGNORE
OWN_ACCOUNT_TRANSFER
HOUSEHOLD_TRANSFER
INVESTMENT_TRANSFER
PREPARE_SNAPSHOT
TRANSACTION_MISSING
LEAVE_UNALLOCATED
PRIMARY_SALARY
ORDINARY_INCOME
```

Generative extraction remains appropriate when arbitrary supporting fields are needed, e.g.:

```text
SET_PAY_DATE
COMPLETE_BANK_FACTS
ALLOCATE_RETAINED_BALANCE with new allocation values
SET_WEALTH_ACCOUNT with arbitrary account hint
RECORD_ASSET_PURCHASE with missing account/time facts
```

Do not create duplicate semantic authority between Jev and generative tools.

---

# 17. P1 — READ Failure Policy

Mutation decisions must fail closed.

READ-only routing has a different risk profile.

Define explicit fallback classes:

## Mutation

```text
Jev unavailable / invalid
-> no automatic mutation
-> Review / clarification / safe retry
```

## READ

Allowed policy:

```text
Jev unavailable
-> existing generative READ path
-> Go still computes all financial numbers
```

If used, record:

```text
fallback_reason = JUDGMENT_UNAVAILABLE
```

This is a capability fallback, not a fake replacement of Jev probability semantics.

---

# 18. P2 — Bank Email Post-Extraction Judgment

Bank Email still needs generative extraction for arbitrary fields.

After extraction + Go structural validation + deterministic reconciliation, add Jev:

```text
transaction_observed            Noul
amount_supported                Noul
direction_supported             Noul
channel_supported               Noul
counterparty_semantics          Choice when bounded
material_ambiguity              Noul
```

For unresolved transfer accounting semantics, use Jev only after deterministic filters.

Do not ask Jev to generate merchant/date strings.

---

# 19. P2 — Financial Provider Email

After arbitrary extraction, use Jev for bounded classification:

```text
observation_type
is_cashflow_event
is_balance_observation
is_trade_or_asset_purchase
evidence_sufficient
material_ambiguity
review_reason
```

The generative model should move toward extraction-only responsibility.

---

# 20. P2 — Semantic Reconciliation

Hard reconciliation constraints remain deterministic.

Only after Go narrows candidates by:

```text
household
amount
direction
time window
source semantics
```

may Jev answer:

```text
same_real_event? Noul
```

for the remaining candidates.

Never let Jev search the ledger or choose hidden IDs.

---

# 21. P2 — Insight Signal Selection

Go/PostgreSQL computes all canonical metrics.

Before generative prose, ask bounded questions such as:

```text
spending_change_material?           Noul
category_shift_material?            Noul
merchant_concentration_noteworthy?  Noul
cashflow_pattern_noteworthy?        Noul
savings_pattern_noteworthy?         Noul
primary_insight_family              Choice
```

Then:

```text
no material signal -> deterministic concise answer
material signals   -> optional generative prose
```

This removes generative calls whose only output is effectively "nothing notable".

---

# 22. P2 — Document Post-Vision Judgment

Do not use Jev for raw image understanding.

Keep vision extraction generative.

When reliable text/structured observations exist, Jev may own:

```text
document family
field support
field ambiguity
evidence sufficiency
review reason
```

Never state that Jev verified pixels or evidence it did not receive.

---

# 23. Observability Must Measure Jev Value

Current telemetry records basic System One call metadata.

Add per-decision metadata:

```text
decision_task
question_keys
policy_version
actual_model_version
decision_outcome
jev_only
jev_then_generative
generative_only
fallback_reason
native_tool_calls_avoided
review_reason
```

Aggregate:

```text
telegram_turns_total
telegram_turns_jev_only
telegram_turns_jev_then_generative
telegram_turns_generative_only
telegram_mutations_jev_only
native_tool_calls_avoided
judgment_error_rate
judgment_latency
generative_latency
review_rate
clarification_rate
```

Do not log raw financial text.

---

# 24. Model and Policy Versioning

`jev-latest` can move to a newer serving model.

For mutation/review-affecting decisions preserve:

```text
actual versioned model
decision task
question key
selected value
relevant probability/confidence
policy version
final Go outcome
```

Threshold tuning must be attributable to the model version that produced the decisions.

---

# 25. Centralize Threshold Policy

Latest code uses values such as:

```text
0.85 / 0.20
0.80 / 0.15
```

inline.

Create a small workflow policy layer.

Example:

```go
type ChoicePolicy struct {
    MinTop        float64
    MinMargin     float64
    MinConfidence float64
}

type NoulPolicy struct {
    High float64
    Low  float64
}
```

Different tasks may require different policies:

```text
READ route
category
salary choice
review reclassification
merchant explicit consent
transfer purpose
semantic reconciliation
```

Version the policy used for auditable decisions.

---

# 26. Reduce the Generative Tool Surface

After each Jev-owned workflow is stable, remove or narrow duplicate generative tools.

Status: the bounded tools for server-owned workflows
(`resolve_salary_choice`, `resolve_merchant_learning`, `confirm_pending_action`,
`cancel_pending_action`, `pending_batch_decision`, fact-free `resolve_review`) are
**fallback-only** when the judgment plane is configured.
`tryJudgmentBoundWorkflow` consumes those turns before the generative loop runs,
so the model never reaches those definitions for a Jev-owned turn. Each tool is
still registered so the workflow-policy filter and degraded-mode surface keep
working, and `update_pending_batch` stays generative because a batch update
needs arbitrary replacement values. `TestJevOwnedWorkflowsPreemptTheirGenerativeTools`
pins this reachability.

Strong candidates already substantially replaced by PR #93:

```text
resolve_salary_choice
resolve_merchant_learning
simple pending-action decision
simple pending-batch decision
fact-free review action selection
finance route selection
category classification authority
```

Keep generative tools only where arbitrary data is required.

Examples:

```text
update_pending_batch replacement values
record_transaction fallback for arbitrary merchant/description/date
correction with arbitrary replacement description
review actions requiring arbitrary account/date/allocation facts
```

The goal is fewer tool definitions, smaller prompts, less JSON decoding, and fewer schema retries.

---

# 27. Target Telegram Orchestration

```text
Telegram text
      |
      v
Go auth + current server workflow
      |
      v
generic candidate harvesting
      |
      v
ONE Jev decision bundle
      |
      +--> route
      +--> period
      +--> transaction type
      +--> category
      +--> support/ambiguity Nouls
      |
      v
Go can complete?
   /          \
 yes           no
  |             |
  |             +--> arbitrary extraction/reasoning needed
  |                         |
  |                         v
  |                  generative agent
  |                         |
  |                  Jev again only if a new
  |                  bounded decision remains
  |
  v
Go validation / binding / reconciliation
   |
   +--> canonical action
   |
   +--> Review / clarification
```

Avoid unnecessary serial Jev calls when the questions can share one state.

---

# 28. Updated Implementation Order

## P0 — Correct and optimize the foundation

1. native typed Noul/Choice/Score request contract;
2. native primitive answer contract;
3. require Choice criteria;
4. strict probability/confidence validation;
5. real LiteRouter -> TypeSafe smoke;
6. fix READ period correctness;
7. bundle route + period + speculative bounded questions;
8. dynamic server-state criteria;
9. focused regression tests.

## P1 — Finish Telegram semantic ownership

1. merchant consent Noul;
2. simple transaction support/ambiguity questions;
3. remove generative confidence from mutation authority;
4. finish category provenance/cleanup;
5. transfer purpose Jev;
6. explicit READ fallback policy;
7. centralized/versioned thresholds;
8. shrink duplicate bounded generative tools;
9. decision-value telemetry.

## P2 — Extend beyond Telegram

1. Bank Email post-extraction verification;
2. Financial Provider Email classification;
3. semantic reconciliation after deterministic narrowing;
4. insight signal selection;
5. post-vision document judgments when evidence permits.

---

# 29. Required Regression Tests

## Native contract

- Choice serializes a criteria map.
- Choice without criteria fails locally.
- Noul serializes optional true/false criteria.
- Score serializes ordered levels.
- Choice decodes choice/probabilities/confidence.
- Noul decodes noul.
- Score decodes score/legend/probabilities/confidence.
- answer type mismatch fails.
- probability-key mismatch fails.
- invalid probability distribution fails.
- real LiteRouter smoke succeeds.

## READ correctness

- "pengeluaran hari ini" -> TODAY.
- "pengeluaran minggu ini" -> THIS_WEEK.
- "cashflow bulan lalu" -> LAST_MONTH.
- "tabungan sejak gajian terakhir" -> CURRENT_CYCLE.
- custom dates never silently become THIS_MONTH.
- wealth ignores period.

## Preserve PR #93 behavior

- pending correction confirm/cancel stays Jev-first;
- pending batch CONFIRM/CANCEL/DEFER stays Jev-first;
- pending batch UPDATE reaches generative replacement extraction;
- salary choice stays Jev-first;
- merchant learning stays bounded;
- bound review cannot escape its server action list;
- stale review binding cannot mutate;
- one-amount simple transaction can avoid generative;
- multiple amounts fall back safely;
- confirmed merchant rule beats Jev category.

## Transaction policy

- unsupported amount prevents mutation;
- type ambiguity prevents mutation;
- OTHER_OR_UNCLEAR category routes review;
- low category margin routes review;
- generative confidence alone cannot auto-confirm.

## Failure policy

- Jev outage on mutation -> no automatic mutation.
- READ fallback follows explicit policy.
- malformed System One response cannot reach mutation code.
- source event remains recoverable/reviewable.

---

# 30. Completion Criteria

The Jev design is materially complete when:

1. Richmod matches the real native System One contract.
2. LiteRouter/TypeSafe interoperability is verified end-to-end.
3. Telegram READ fast paths honor the requested period.
4. a common Telegram turn normally needs at most one initial System One call.
5. PR #93 bounded server-state workflows remain Jev-first.
6. simple explicit transactions complete without generative inference when candidates are sufficient.
7. category classification is fully Jev-owned after deterministic rules.
8. transfer-purpose classification is Jev-owned when deterministic rules cannot decide.
9. generative self-confidence no longer authorizes transaction auto-confirm.
10. Bank/Provider Email bounded semantics use Jev after extraction.
11. semantic reconciliation uses Jev only after hard narrowing.
12. insight generation uses Jev to select/filter signals before optional prose.
13. telemetry reports Jev-only vs mixed vs generative turns and native-tool calls avoided.
14. actual Jev model version and Richmod policy version are auditable.
15. duplicate bounded generative tools are removed or explicitly fallback-only.
16. Go/PostgreSQL remain the sole canonical authority.

---

# 31. Non-Goals

"Use Jev maximally" does not mean sending deterministic work to Jev.

Do not use Jev for:

- arithmetic;
- household authorization;
- exact callback/reply identity;
- SQL;
- deterministic date math after a period is known;
- exact account matches;
- hard reconciliation constraints;
- raw image understanding;
- arbitrary string generation;
- long-form user-facing prose.

Use Jev maximally when the problem is:

```text
semantic
+
bounded
+
probabilistic
+
software branches on uncertainty
```

That is the boundary that reduces both generative LLM work and brittle fuzzy Go logic.

---

# 32. Contract References

Implementation must verify against current provider documentation before coding:

- TypeSafe OpenAPI / Swagger: `https://api.typesafe.ai/docs`
- TypeSafe official JavaScript SDK: `@typesafe-ai/sdk`
- TypeSafe System One/Jev product documentation.

Current public documentation describes:

- Noul as a yes/no probability;
- Choice as a selected option plus probabilities over defined options and confidence;
- Score as an ordered-scale score plus probabilities and confidence;
- multiple questions evaluated independently in parallel against the same state.

Provider documentation is authoritative if any old Richmod PRD/example differs.
