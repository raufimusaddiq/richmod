# ADR-038: System One semantic decision plane

## Status

Accepted for implementation — 2026-09-20.

## Context

Richmod already separates model understanding from canonical financial authority:
PostgreSQL is canonical and Go owns authorization, deterministic validation,
reconciliation, review, audit, and persistence.

The existing model boundary is optimized around generative LLMs exposed through
LiteRouter's OpenAI-compatible Responses / Chat Completions protocols. This is
appropriate for arbitrary extraction, vision, multi-step reasoning, and prose.

A large subset of current AI work is different. Richmod frequently needs one
answer from a bounded set, or a probability about a semantic predicate:

- transaction type;
- category;
- transfer purpose;
- review action;
- salary choice;
- merchant-learning consent;
- finance scope;
- whether evidence supports a proposed fact;
- whether two descriptions likely refer to the same real event.

Several of these decisions are currently represented as generative native tool
calls plus model-reported confidence. That makes a generative model produce and
grade its own decision, and requires function-call generation, JSON argument
decoding, schema validation, retries, and prompt machinery even when the software
already knows the complete output domain.

LiteRouter now exposes TypeSafe System One natively at `POST /v1/systemone`.
The endpoint preserves the provider-native `state + questions` contract and keeps
the upstream TypeSafe credential inside LiteRouter.

## Decision

Richmod adopts Jev / System One as the primary bounded semantic decision plane.

The hierarchy is:

```text
1. deterministic Go
2. Jev / System One for bounded semantic judgment
3. generative LLM for unbounded extraction, reasoning, vision, or prose
4. Go remains final authority for every canonical transition
```

Jev is not merely a post-LLM verifier. When a workflow can be completed from
server state, deterministic candidates, and typed System One decisions, Richmod
SHOULD skip the generative LLM entirely.

Examples include:

- bound review replies selecting among currently allowed actions;
- salary choice;
- explicit merchant-learning consent;
- pending batch decision;
- category classification after deterministic merchant/category rules;
- transfer-purpose classification after deterministic account/reconciliation
  rules;
- simple Telegram READ routing;
- common explicit transaction commands when generic candidate harvesting
  supplies the required amount/date/account candidates.

The generative LLM remains the correct tool for arbitrary values that Jev cannot
produce, such as unknown merchant strings, free-form descriptions, arbitrary
document fields, vision extraction, multi-step conversational retrieval, and
user-facing prose.

After generative extraction, any remaining bounded semantic choice SHOULD return
to Jev rather than rely on generative self-reported confidence.

## Gateway boundary

All external model inference remains behind LiteRouter.

```text
Richmod -> LiteRouter /v1/responses -> generative providers
Richmod -> LiteRouter /v1/systemone -> TypeSafe Jev
```

Richmod MUST NOT call TypeSafe directly and MUST NOT store the upstream TypeSafe
API key. The System One client reuses Richmod's LiteRouter base URL and client
API credential.

System One remains a distinct protocol. Richmod and LiteRouter MUST NOT translate
Jev into a fake OpenAI chat/Responses model.

Automatic fallback from a failed Jev decision to a generative model answering
the same bounded question is prohibited. The semantics differ, and silent
fallback would make thresholds and audit behavior inconsistent.

## Authority boundary

Jev may choose or score semantic answers only from server-owned questions and
candidate sets.

Jev MUST NOT:

- choose hidden canonical database identifiers;
- bypass household authorization;
- override exact Telegram callback or reply binding;
- override deterministic arithmetic;
- override exact account/entity matches;
- override deterministic reconciliation constraints;
- mutate PostgreSQL directly.

Go validates every Jev result against the current server state before any
canonical transition.

## Candidate harvesting

Go may harvest generic candidates without deciding their semantics, including:

- IDR amount tokens;
- relative/explicit date tokens;
- time tokens;
- known account aliases;
- known merchant aliases;
- active category slugs;
- opaque transaction references;
- currently allowed workflow actions.

Candidate harvesting MUST remain generic and MUST NOT introduce bank-template or
provider-specific branches. If required arbitrary data cannot be represented as
candidates, the workflow may invoke the generative extractor or ask for
clarification.

## Confidence policy

Generative `confidence` and `category_confidence` are non-authoritative on
Jev-enabled workflows and should be removed from decision logic as those paths
migrate.

Richmod owns all probability thresholds, margins, policy versions, and review
rules. Model probability is a signal consumed by Go policy, never a canonical
truth value.

## Failure behavior

If a Jev-owned decision cannot be obtained because of timeout, gateway failure,
rate limiting, malformed output, missing answers, or invalid probabilities,
Richmod must not silently trust the generative model instead.

Allowed outcomes are:

- deterministic resolution when the answer is still exact;
- clarification;
- Review Inbox;
- safe background-job retry;
- generative processing only when the task genuinely requires a different
  unbounded capability.

Deterministic flows remain usable when model inference is unavailable.

## Consequences

### Amendment — production requirement and safe degradation (2026-09)

`JUDGMENT_MODEL` is a production configuration invariant, not an optional
fast-path switch. Because Jev owns bounded semantic mutation authority, the
worker refuses to start when the model is unset; non-production environments opt
out explicitly with `JUDGMENT_MODE=disabled-dev`. Omitting an environment
variable must never silently reopen a different mutation authority.

Availability is a separate state from configuration. When a configured Jev is
unreachable:

- deterministic features and READ tools keep working;
- a READ question may still be served by a generative READ-only agent;
- the conversational tool surface exposes no mutation tool, so no hidden LLM
  semantic authority appears;
- a mutation request returns an explicit non-success response and preserves
  evidence/proposal state instead of confirming.

Every Jev-influenced canonical mutation also records bounded decision provenance
(`judgment_decision`: task, model, policy version, question keys, bounded answer
summary, outcome) in the same transaction as the mutation. Raw user text, email
bodies, document bytes, and credentials are never stored there.

- Simple Telegram requests can avoid generative inference entirely.
- Bounded native-tool calls can be removed from the conversational LLM surface.
- Category and transfer-purpose decisions gain explicit probability
  distributions instead of model self-confidence.
- Prompt and tool schemas become smaller.
- Native-tool JSON decode/retry work decreases.
- Generative models are reserved for the tasks where string generation and
  open-ended reasoning provide value.
- System One becomes a production dependency for the workflows that adopt it,
  so latency, error rates, review rates, and generative calls avoided must be
  measured.
- PostgreSQL and Go remain the financial authority.

## Related decisions

### Amendment — implicit workflow bindings and lane selection (2026-09)

The original decision let Go narrow the conversational tool surface from a
server-bound workflow target *before* asking Jev anything about the current
message. Two classes of binding must be separated, because they mean different
things about the user's text:

- **Exact binding** — the user's message is part of the workflow by
  construction: a pending correction, a pending batch, a pending salary choice,
  or an explicit Telegram reply to the workflow's own message. Here an
  unreadable answer means "the reply to this workflow is unclear", so a
  clarification hard stop is correct.
- **Implicit binding** — the workflow was inferred from chat state alone: an
  open review, or an awaiting merchant-learning confirmation. Here the user's
  message may be an entirely new event, so a bounded answer of "not this
  workflow" must release the binding and fall through to normal handling.

Keying both classes on a single `!ok || answer == OTHER_OR_UNCLEAR` guard is
**prohibited**. It is the defect that let one open review swallow every later
message in a chat (an expense was never recorded; the turn died with 0 model
phases). The required distinction is:

```text
classifier error / undecided   -> fail closed (clarification or safe stop)
decided "not this workflow"    -> release implicit binding, fall through
                              -> keep hard stop for exact bindings only
```

#### Risk: implicit bindings select the lane before the route call

Even with the fall-through above, Go still chooses the mutation lane from chat
state before Jev sees the current message. Implicit bindings therefore remain a
standing hazard: any chat that accumulates open reviews (a real household had
six) puts every non-review message at risk, and each new implicit binding adds a
special case to `tryJudgmentBoundWorkflow`.

The durable shape is **route-first lane selection**: run the existing bounded
route decision (`judgmentTaskRoute`, whose vocabulary already includes
`REVIEW_INTERACTION`, `MERCHANT_LEARNING_INTERACTION`, and
`SALARY_INTERACTION`) and narrow the tool surface only when the chosen route
names the bound workflow. Under that shape an implicit binding can no longer
mis-own a turn, because the route call — not chat state — decides whether the
turn is a workflow reply at all.

Until route-first selection is adopted, every implicit binding MUST follow the
three-way rule above, MUST carry a test proving a new-event message falls
through, and MUST NOT be introduced without one. See
`docs/plans/jev-go-implicit-binding.md` for the staged work.

- ADR-005: Cloud LLM Gateway boundary — amended to include native System One.
- ADR-024: Native finance tool calls — remains applicable to generative paths
  that still require native tools.
- ADR-030: Native-only model tool contract — remains applicable to strict
  generative extraction/classification paths not migrated to System One.
- ADR-033: Bounded natural conversational finance agent — amended in practice by
  allowing a Jev-first fast path before entering the generative agent.
- ADR-037: Bounded intelligent document interpretation — vision extraction stays
  generative until sufficient text-backed evidence exists for System One
  judgments.
