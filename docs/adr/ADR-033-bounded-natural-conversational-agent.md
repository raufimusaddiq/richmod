# ADR-033: Bounded natural conversational finance agent

## Status

Accepted — 2026-09-12.

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

1. **Final text** — non-empty assistant text and zero tool calls. Text is display
   only and is never parsed into a financial action.
2. **READ batch** — one or more allow-listed READ tool calls. Independent reads
   may execute concurrently. Their model-safe authoritative results are supplied
   to the next bounded model phase.
3. **SIDE EFFECT** — exactly one allow-listed side-effect tool call and no other
   calls. Go strictly decodes and validates it and owns the resulting domain
   transition. At most one side effect may execute in one free-text user turn.

A response mixing READ and SIDE EFFECT calls, containing multiple side effects,
using an unavailable/unknown tool, or containing malformed arguments is rejected
before any tool in that response executes. Tool availability is server-state
specific: a globally known tool is still rejected if it was not exposed for the
current pending/review state.

The conversational model may perform several bounded phases so a later READ can
depend on an earlier result. Initial production limits are:

```text
max model phases per user turn:        5
max read calls per model response:     4
max read calls per user turn:          8
max side effects per user turn:        1
per model call timeout:                8 seconds
overall Telegram free-text budget:    20 seconds
```

The limits are server-owned and may be tuned without changing the canonical
ledger boundary.

Normal final responses and clarifying questions are ordinary model text. A fake
`respond_to_user` or `ask_clarification` tool is not required merely to satisfy a
framework contract. The model must ask only for facts genuinely missing from
bounded conversation and server-owned workflow context.

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

Merchant-learning confirmations follow the same rule: an exact replied review
wins; otherwise exactly one pending merchant-learning review is required.

Opaque transaction references are scoped to household, Telegram identity, chat,
and expiry. Parallel READ batches use collision-safe prefixed references so two
concurrent searches cannot redefine the same short reference.

Post-mutation response synthesis receives only the model-safe authoritative
mutation result and no side-effect tools. Failure to synthesize does not roll
back or retry the mutation; Go sends a deterministic acknowledgement instead.

The strict single-native-tool contract remains available for non-conversational
lanes such as classification/extraction where a single typed result is the
correct interface. Conversation, classification, and extraction are explicitly
allowed to use different LLM contracts.

## Superseded / amended decisions

- ADR-030's requirement that **every finance model invocation** contain exactly
  one native tool call is superseded for the Telegram conversational lane.
  ADR-030 remains applicable to strict single-result finance model workflows.
- ADR-030's universal `tool_choice=required` and universal parallel-call ban are
  superseded for the Telegram conversational lane. Conversation uses automatic
  tool choice; parallel execution is allowed only for validated READ batches.
- ADR-031's consequence that one native tool decision terminates a free-text
  model phase/turn is superseded. Multiple bounded model phases are allowed.
- ADR-027's 10-second Telegram budget is amended for free-text conversation to a
  20-second overall turn budget with an 8-second per-model-call limit. Other
  task budgets remain unchanged.

## Consequences

- Richmod can answer ordinary conversational follow-ups without inventing a tool
  call.
- Analytical questions can retrieve several independent facts in one model
  response and can perform dependent follow-up reads in later phases.
- The LLM performs materially more useful conversational reasoning while never
  gaining authority over canonical financial state.
- Normal free-text finance answers are model-written and grounded in
  authoritative Go tool results; deterministic canned query responses become
  fallback/system behavior rather than the normal path.
- Multiple side effects in one free-text turn are structurally impossible at the
  server policy boundary, not merely discouraged by the prompt.
- Registered side effects must map to exactly one conversational executor; there
  is no implicit fallback from the agent to a legacy canned-reply mutation path.
- Existing deterministic callback/review bindings remain valid and exact reply
  targets have precedence over implicit review selection.
- The document classification/extraction lanes are not changed by this ADR.
