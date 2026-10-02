# ADR-033: Bounded natural conversational finance agent

## Status

Accepted — 2026-09-12; amended by ADR-038.

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
per model call timeout:               25 seconds
overall Telegram free-text turn:     130 seconds (queue budget 160 seconds)
progress notice after:                10 seconds
attempts after a model timeout:        2
```

The limits are server-owned and may be tuned without changing the canonical
ledger boundary.

Every model call has the same cap. An earlier version capped the first call
(choosing tools) at 8 seconds and only the answer call at 25, on the assumption
that tool selection is fast (p50 about 3.5 seconds). That failed in use: a short
follow-up carries the previous answer as context, its first call measured 6
seconds, and it timed out twice in a row at 8 seconds without ever choosing a
tool. Long-form analytics prose measures 9 to 11 seconds. The cap exists to bound
a hung call, not to ration a slow one. A timeout repeats with the same prompt and
the same cap, so a typed message is tried twice, not five times.

The turn is bounded by its phases, not by a wall clock tuned to one question: the
turn timeout covers every phase at the call cap (5 x 25 s = 125 s), so a
multi-read analytics turn is not cut short.
Because such a turn can run for a while, a turn still running after 10 seconds
sends the household one plain reply, "Masih kuproses ya, analisis seperti ini
butuh waktu lebih lama. Jawabannya menyusul di sini.", and the answer follows as
a normal message. The notice is queued only if no reply to that message exists
yet, which makes it idempotent across retries and keeps it from landing after the
answer; turns that finish within 10 seconds (single-phase turns measure p90 about
6 s) never send one. Typed messages are handled by two chat workers by default,
so a long turn holds one of them for its duration.

### Conversation memory

Each message is its own turn: there is no session, so nothing expires with the
conversation and the time limits above reset with every message. What the agent
remembers is the stored turns of the same chat inside a window, loaded oldest
first and compacted:

```text
window:                  24 hours   (was 60 minutes)
rows read per turn:      40         (was 20)
kept whole:              the newest 6 rows, including tool results
older rows:              user and assistant text only, clipped to 200 / 300
                         characters, tool results dropped, marked "compacted"
text budget:             6,000 characters; the oldest compacted rows go first,
                         the newest rows are never dropped
```

A follow-up the next morning still works, and the prompt stays bounded however
long the chat is. The compaction is deterministic on purpose. A model-written
summary would put untrusted numbers into later prompts in a finance product, add a
model call to every turn, and add one more call that can time out; Go trims text
and drops bulky tool data instead, and exact figures are fetched again by READ
tools when they are needed. A retried message is saved once: the USER and
ASSISTANT rows are written once per source event, while TOOL rows (one per call)
are not deduplicated.

A typed message is never left without an answer. When it will not be retried
again (a model timeout on the second attempt, or any failure on the last
attempt), the notice is queued in the same transaction that marks the job
`FAILED`: Go marks the source event `FAILED` and queues one plain reply saying
the assistant was too slow and to try again. Either both happen or neither does
(the stale-lock claim then retries the job). An event that already reached a
final state is left alone, the event is claimed in that transaction so two
workers cannot both send the reply, and only the worker that still owns the job
may run the step. If the step itself cannot succeed, the job is still marked
`FAILED` without it, because a job that can never be failed would be reclaimed
forever.

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
  130-second overall turn backstop with one 25-second limit per model call, plus a
  progress notice after 10 seconds (see the limits above). Other task budgets
  remain unchanged.

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
