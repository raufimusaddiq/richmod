# ADR-033: Bounded natural conversational finance agent

## Status

Accepted — 2026-09-12; amended by ADR-038 and ADR-030 (2026-09-30 rendering amendment).

## Context

Richmod's deterministic finance core is a product invariant: PostgreSQL is the
canonical state and Go owns authorization, validation, reconciliation, review,
audit, and every canonical financial mutation.

The previous Telegram LLM contract extended that determinism into the dialogue
layer. Every model response was forced to contain exactly one native tool call,
one model decision effectively ended a free-text turn, and Go normally rendered
the final query response. This made the LLM behave mainly as an intent classifier
and slot extractor. It could not naturally inspect several authoritative facts,
request another read after seeing a result, or synthesize a contextual answer.

That restriction did not materially improve ledger safety. The safety boundary
is whether model output can change canonical financial state, not whether the
assistant is allowed to speak normally or perform several read-only lookups.

## Decision

Telegram free text uses a bounded conversational agent implemented in Go.

The primary invariant is:

```text
LLM may understand, retrieve, reason, explain, and propose.
Go authorizes, validates, reconciles, reviews, audits, and commits.
PostgreSQL remains canonical.
```

A conversational model response is valid in exactly one of these shapes:

1. **RENDER** — exactly one allow-listed display-only rendering tool such as
   `respond_to_user`. Its `message` may be natural free-form prose. Go
   validates the tool envelope and forwards the message; it never parses the
   message into financial state.
2. **READ batch** — one or more allow-listed READ tool calls. Independent reads
   may execute concurrently. Their model-safe authoritative results are supplied
   to the next bounded model phase.
3. **SIDE EFFECT** — exactly one allow-listed side-effect tool call and no other
   calls. Go strictly decodes and validates it and owns the resulting domain
   transition. At most one side effect may execute in one free-text user turn.

Raw final assistant text with zero tool calls is no longer a production response
shape. Native tools are the machine boundary; prose remains natural inside the
RENDER tool.
Pending transaction batches are a stricter workflow lane: while one is active,
the model must call the server-exposed `pending_batch_decision` tool for every
reply. The gateway uses a named required tool choice. `CONFIRM`, `CANCEL`, and
`UPDATE` mutate only the bound batch; `DEFER` preserves it for unrelated
questions. Plain assistant text is invalid before this decision.

A response mixing READ and SIDE EFFECT calls, containing multiple side effects,
using an unavailable/unknown tool, or containing malformed arguments is rejected
before any tool in that response executes. Tool availability is server-state
specific: a globally known tool is still rejected if it was not exposed for the
current pending/review state.

The conversational model may perform several bounded phases so a later READ can
depend on an earlier result. Initial production limits are:

```text
max model phases per user turn:        5
max read calls per model response:     5
max read calls per user turn:          8
max side effects per user turn:        1
per model call timeout:                8 seconds
overall Telegram free-text budget:    20 seconds
```

The limits are server-owned and may be tuned without changing the canonical
ledger boundary.

Normal final responses and clarifying questions use the native rendering tool.
This is not intended to structure the language itself: the message remains
free-form prose. The tool exists to keep the provider/model boundary native and
to prevent application code from parsing arbitrary model output.

The model must ask only for facts genuinely missing from bounded conversation and
server-owned workflow context.

Read tools return composable structured finance facts rather than pre-rendered
Telegram strings. The model decides which authoritative facts it needs and
synthesizes the normal user-facing answer. Go remains responsible for numerical
truth and all canonical calculations.

### Server-owned target binding

A side-effect target is resolved by Go before the model is allowed to act on it.
The binding precedence is:

1. exact Telegram callback or `reply_to_message_id`;
2. exact pending action/batch or other unique server workflow state;
3. one uniquely eligible active review;
4. opaque server-scoped references returned by prior READ tools;
5. model interpretation only for the semantic change being proposed, never for
   choosing a hidden canonical identifier.

Exact reply binding must never be displaced by a newer or unrelated review.
When no exact reply exists, more than one eligible target is ambiguous and the
mutation tool is not exposed. Bound canonical IDs remain server-only and are
revalidated immediately before mutation, so a later row becoming "latest" cannot
redirect an already-bound action. Stale bindings fail closed.

Review eligibility is scoped to the Telegram recipient/chat. A review belonging
to another member or chat in the same household must not participate in the
current user's ambiguity set. Transfer-reconciliation and Wealth-observation
mutations revalidate the bound target ID, household, open review/case state,
recipient chat, and exact reply message when present **inside the same database
transaction that performs the mutation**. Those review/request/recipient/subject
rows are locked for the mutation so the binding cannot drift after validation.

`IGNORE` is a dismissal, not a successful reconciliation. For transfer
reconciliation, ignoring the review dismisses the reconciliation case, resolves
the review with an `IGNORED` resolution, and marks the original source ignored;
it must never record `TRANSFER_RECONCILED` or imply that a canonical transfer was
successfully reconciled.

Merchant-learning confirmations follow the same rule: an exact replied review
wins; otherwise exactly one pending merchant-learning review is required.

Opaque transaction references are scoped to household, Telegram identity, chat,
and expiry. Parallel READ batches use collision-safe prefixed references so two
concurrent searches cannot redefine the same short reference.

Post-mutation response synthesis receives only the model-safe authoritative
mutation result and the rendering tool, with no side-effect tools. Failure to
synthesize does not roll back or retry the mutation; Go may send a literal
deterministic acknowledgement of the already-completed result. That fallback may
report status but must not perform new semantic interpretation or analytical
reasoning.

The strict single-native-tool contract remains available for non-conversational
lanes such as classification/extraction where a single typed result is the
correct interface. Conversation, classification, and extraction are explicitly
allowed to use different LLM contracts.

## Superseded / amended decisions

- ADR-030's strict **one call total** shape remains superseded for the Telegram
  conversational lane because conversation may use bounded multi-phase READ
  batches.
- ADR-030's 2026-09-30 amendment applies to every conversational phase: the
  provider response must use native tools, and final user-facing prose is emitted
  through a native RENDER tool rather than raw assistant text.
- Conversation may still execute multiple validated READ calls in a bounded
  phase. Side effects remain exactly one. Rendering is exactly one display-only
  tool call.
- ADR-031's consequence that one native tool decision terminates a free-text
  model phase/turn is superseded. Multiple bounded model phases are allowed.
- ADR-027's 10-second Telegram budget is amended for free-text conversation to a
  20-second overall turn budget with an 8-second per-model-call limit. Other
  task budgets remain unchanged.

## ADR-038 amendment

System One/Jev may handle bounded Telegram routing and server-state decisions
before a turn enters this generative conversational loop. When deterministic
context plus typed Jev decisions are sufficient, Richmod may complete a READ or
side-effect workflow without invoking the conversational model. This does not
change the server-owned binding, authorization, one-side-effect, reconciliation,
or canonical-state rules in this ADR.

The conversational agent remains the fallback/primary path for turns requiring
arbitrary extraction, dependent READ reasoning, open-ended synthesis, or prose.

## Consequences

- Richmod can answer ordinary conversational follow-ups naturally while keeping
  the provider boundary native through a rendering tool.
- Analytical questions can retrieve several independent facts in one model
  response and can perform dependent follow-up reads in later phases.
- The LLM performs materially more useful conversational reasoning while never
  gaining authority over canonical financial state.
- Normal finance answers are model-written inside a native rendering tool and
  grounded in authoritative Go tool results; deterministic canned analytical
  responses are prohibited as an intelligence substitute.
- Multiple side effects in one free-text turn are structurally impossible at the
  server policy boundary, not merely discouraged by the prompt.
- Registered side effects must map to exactly one conversational executor; there
  is no implicit fallback from the agent to a legacy canned-reply mutation path.
- Existing deterministic callback/review bindings remain valid and exact reply
  targets have precedence over implicit review selection.
- The document classification/extraction lanes are not changed by this ADR.


## Anti-Go semantic drift amendment — 2026-09-30

The bounded conversational agent exists so Go does not become a hidden NLP or
analysis engine.

Go may implement exact deterministic policy, validation, authorization,
arithmetic, binding, persistence, and literal protocol/status messages.

Go must not implement a semantic fallback using:

- keyword or substring intent detection;
- regex-based interpretation of ordinary language;
- switch/case branches that decide conversational meaning;
- hard-coded narrative selection intended to imitate model reasoning;
- template-generated analytical conclusions;
- open-ended "noteworthy" judgments encoded as arbitrary deterministic branches.

If implementation needs to understand what a human sentence means, decide what
is worth discussing, or synthesize an analytical explanation, that is an
intelligence responsibility. Use Jev for a genuinely bounded semantic decision
or generative intelligence for open-ended reasoning/prose.

A provider failure does not transfer semantic ownership to Go.
