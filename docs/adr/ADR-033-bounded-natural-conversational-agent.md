# ADR-033: Bounded natural conversational finance agent

## Status

Accepted — 2026-09-12; amended by ADR-038; evidence context extends it in ADR-050;
amended 2026-11-05 (agent loop hardening, see the amendment at the end).

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

Pending transaction batches expose one server-bound tool, `pending_batch_decision`
(`CONFIRM`, `CANCEL`, `UPDATE` of one `item_ref`, optionally confirming after the
update), which mutates only the bound batch. The model calls it only when the
message is about that batch; any other message is an ordinary turn and the batch
stays pending (2026-11 amendment; the earlier "call it for every reply, `DEFER`
unrelated questions" rule made unrelated questions go unanswered).

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
6 s) never send one. Typed messages run on the DEFAULT lane's three loops; the
claim serializes one household's Telegram text (2026-11 amendment), so a long
turn holds one loop and never delays another household.

The queue budget (160 s) and the five-minute job lease hold per attempt, not per
turn: a message whose first attempt times out is tried once more, so the worst
case is about 322 s end to end, which is past the lease. If the first worker is
still finishing when the lease expires, a second attempt can run alongside it;
that is safe for the turn rows (see Conversation memory) and for the terminal
notice, which is claimed in one transaction.

### Conversation memory

Each message is its own turn: there is no session, so nothing expires with the
conversation and the time limits above reset with every message. What the agent
remembers is the stored turns of the same chat inside a window, loaded oldest
first and compacted:

```text
window:                  24 hours   (was 60 minutes)
rows read per turn:      40         (was 20)
kept whole:              the newest 3 user exchanges, including tool results
                         (was: the newest 6 rows; 2026-11 amendment)
older rows:              user and assistant text only, clipped to 200 / 300
                         characters, tool results dropped, marked "compacted"
memory budget:           6,000 characters (not bytes) of text plus tool JSON; tool
                         results of older verbatim exchanges go first, then the
                         oldest compacted rows; the newest exchange is never trimmed
```

A follow-up the next morning still works, and the prompt stays bounded however
long the chat is. The compaction is deterministic on purpose. A model-written
summary would put untrusted numbers into later prompts in a finance product, add a
model call to every turn, and add one more call that can time out; Go trims text
and drops bulky tool data instead, and exact figures are fetched again by READ
tools when they are needed. A retried message is saved once: the USER and
ASSISTANT rows are written once per source event, while TOOL rows (one per call)
are not deduplicated. The guarantee holds under concurrency (two workers after a
stale-lock reclaim) through a transaction-scoped advisory lock on the event and
role; a unique index would need a migration that deletes the duplicates retries
already left behind, so it is not used.

### Turn-scoped analytics refs

The analytics READ tools issue opaque refs (`category.3`, `category.3.merchant.1`).
They are positions in one turn's fact snapshot, so they are valid only in the turn
whose `get_cycle_changes` issued them: reusing one in a later turn could point at
a different category after the data changed, which in a finance product must never
work silently. Three rules keep follow-ups working without that risk:

- **Replay.** A stored tool result is replayed to the model without its analytics
  refs; names, figures, and the server-bound `tx_`/`review_` refs stay, so
  "makan di luar lumayan gede juga?" can still refer to what was just said.
- **Prompt.** The model is told refs from earlier turns are expired and to call
  `get_cycle_changes` again before `get_category_drivers` or
  `get_supporting_transactions`.
- **Recoverable error.** A ref the session never issued returns a tool result
  (`REFERENCE_NOT_ISSUED`, with the next step) instead of failing the turn, so the
  model fixes it inside the phase budget. Unservable periods (no salary cycle, a
  range over a year) and expired transaction refs are handled the same way
  (2026-11 amendment). Other read errors still end the turn.

A turn that still fails tells the household the cause: a model timeout says the
assistant was slow, an unreachable gateway says the AI cannot be reached, any other
failure says the system had a problem (never the household's wording), every
notice says nothing was recorded, and the Tindakan item records `TIMEOUT`,
`UNAVAILABLE` or `ERROR` accordingly (ADR-049; 2026-11 amendment).

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
Since the 2026-11 amendment synthesis runs only for a result without its own
deterministic copy.

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

## Amendment — agent loop hardening (2026-11-05)

Production (PROCESS_TELEGRAM_TEXT p50 5.5 s, p90 11.3 s, max 37.5 s; 16 of 147
agent calls timed out; 221 USER vs 160 ASSISTANT turns; turn rows dominated by
126 internal `agent_transaction_refs` rows) showed the loop losing memory,
answers and time. The boundary above is unchanged: Go validates and commits, the
model proposes. What changed:

1. **Memory counts exchanges.** The verbatim window is the newest three user
   exchanges, not six rows. Bookkeeping rows (`agent_transaction_refs`, which only
   anchor opaque refs) are never replayed. Tool JSON counts against the 6,000
   character budget. USER/ASSISTANT bookkeeping context (send time, phase
   counters) is not replayed; the send time is kept as `at`.
2. **Failures are turns.** Every failure reply — the terminal notice queued when a
   job gives up and an in-turn refusal — is stored as an ASSISTANT turn marked
   `failure` (with `nothing_recorded`), in the same transaction as the reply, so the
   next turn knows the question went unanswered. The ASSISTANT row of every reply
   commits with the reply itself.
3. **Error taxonomy.** Model-recoverable refusals (a period with no salary cycle,
   a range over 366 days, a malformed custom range, an unknown `item_ref`, an
   expired ref, an unparseable date, an unknown category slug) are returned to the
   model as a tool result with `status` and `next_step` (`agentRecoverable`, at
   most two per turn); date/time shape rules moved into `validateCreate` so the
   ADR-055 repair round covers them. Gateway failures are classified by type:
   unreachable/unconfigured/HTTP 408/429/5xx is *unavailable* (one retry, then a
   terminal notice saying the AI cannot be reached and nothing was recorded;
   ADR-052 amendment adds the deterministic expense fallback), HTTP 4xx is a
   *rejection* (never retried), a model timeout keeps its one retry, anything else
   is transient and keeps the job's attempts. No terminal copy blames the user's
   wording.
4. **Deterministic replies.** A mutation whose result Go fully knows is answered
   from that result without a second model call; only a result without its own
   copy is worded by the model. A recorded transaction shows label, amount,
   category and date; one left for review says the card below needs one step (the
   card goes to the same chat, so the Inbox is not mentioned). The progress notice
   names what the turn is doing (recording, analysing, or thinking); the typing
   indicator covers the whole turn.
5. **Reply atomic with the mutation (T7).** The core executors (record, batch
   stage/update/confirm/cancel, correction stage/resolve, existing-edit offer)
   queue the reply and its ASSISTANT turn inside the mutation's own transaction.
   For every other executor, a retry that finds the event final but unanswered
   (no ASSISTANT turn, no non-progress reply) re-sends the answer from the stored
   side-effect result, once, under the turn lock.
6. **Typed answers bind deterministically (R5).** Without Reply, a message binds
   to an open card only by Go rules: only live cards (OPEN, not expired) of this
   chat count; a card expects a category (`AWAITING_CATEGORY`), a date
   (`AWAITING_DATE`), amount and time (a bank email missing its facts), or a short
   free-text detail (a merchant/purpose question); the text must *be* that answer
   (a household category name/slug with only filler words, a date, an amount with
   only time words, or — for the single free-text card — a short phrase with no
   digit or question). One match binds; several typed matches get a "which card?"
   message with one button per card (`pending:card:<token>`,
   `telegram_pending_card_choice`), and the tap applies the stored value to the
   chosen card through the same Go resolver, with no model call. Otherwise the
   message is new and up to ten live cards are listed as `open_review_cards`
   context only. A plain yes/no binds a pending merchant-learning question;
   anything else is a new message.
7. **Explicit replies.** A Reply to a bot message that is not a workflow message
   (an earlier answer, a batch list) is an ordinary turn with the general catalog.
   A Reply to a closed review card stays narrowed and the model gets
   `explicit_reply_unbound` with instructions; the never-set
   `mutation_authority_unavailable` flag was removed from the prompt.
8. **Request weight.** The household category list is sent once
   (`allowed_category_slugs`) instead of as an enum in every schema. Overlapping
   Telegram-native reads were merged without losing capability: `query_spending`
   carries the category breakdown (was `get_category_breakdown`), `query_cashflow`
   the insight extras (was `get_finance_insight`), `search_transactions` orders by
   `LARGEST` (was `get_largest_transactions`). The four batch tools became one.
   `get_evidence_context` is offered only when the turn has evidence context.
   Identical repeated READ calls in one turn are served from the turn's cache.
   The shared analytics READ tools are unchanged.
9. **Phase cap.** The last allowed model phase of a turn that already read facts
   is offered no tools and told to answer from what was gathered, instead of the
   turn ending with a fixed "ask one part at a time" reply.
10. **Corrections.** `propose_transaction_correction` may also change the amount
    and merchant of a transaction whose evidence is only the household's own
    entries (TELEGRAM_TEXT/TELEGRAM_CALLBACK/WEB_MANUAL); for bank, provider or
    document-backed transactions Go answers `SOURCE_EVIDENCE_FIXES_AMOUNT`. The
    rule is checked again under lock when the button confirms; the change is
    audited with before/after values and refreshes open cycle residuals. Pending
    corrections and batches wait 30 minutes (was 5), and the expired copy says
    what to do.
11. **Batch lane.** A missing or unknown category no longer rejects a batch: the
    item is staged without one and recorded as NEEDS_REVIEW with a review card.
    Items get the single entry's duplicate checks (a similar recent expense is
    shown before confirmation; an item a just-sent document already recorded is
    not recorded again), each confirmed item refreshes open cycle residuals, and
    merchants are resolved to the household merchant identity (merchant memory
    applies, and the merchant-learning question can follow a review). Typed single
    entries resolve merchants the same way.
12. **Per-household serialization.** The DEFAULT lane runs three loops. The claim
    never starts a household's Telegram text job while another of its Telegram
    text jobs in the lane is running (lease younger than five minutes) or while an
    older one is still pending, even one whose retry is not due yet — so a retried
    message is never overtaken by the next one, and other households are served in
    parallel. The migration-00087 fairness order is unchanged.
13. Recent transactions in context list the sending member's own entries first
    (`entered_by_you`).

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
