# ADR-046: Universal review interaction projections

## Status

Accepted architecture direction — 2026-09-25.

Amends:

- ADR-023 Universal review items;
- ADR-028 Canonical universal review orchestration;
- ADR-044 Proposal-first Review Inbox.

Related:

- docs/archive/RICHMOD_UNIVERSAL_REVIEW_INTERACTION_PRD.md
- docs/bdr/BDR-002-review-inbox-channel-independent.md
- ADR-039 Canonical ReviewDecision contract
- ADR-038 / ADR-045 Telegram and intelligence-routing rules

## Context

The canonical review model is already universal.

review_item may point to transactions, proposals, source events, documents,
Wealth observations, financial-email observations, and cycle residual cases.

The Telegram adapter is older.

review_request was introduced with a non-null transaction_id, and current
EnqueueReviewRequest takes a transaction ID. processBoundReview also joins
through transaction.

As a result, review_item is universal while Telegram resolution is still
transaction-centric.

The Web API has also accumulated canonical resolution logic separately from
Telegram handlers.

Continuing to add one-off Telegram review actions would increase semantic drift.

## Decision

Adopt a three-layer review architecture:

~~~text
Layer 1: review_item + ReviewDecision
         canonical review state

Layer 2: Review Resolution Domain Service
         canonical action validation and mutation

Layer 3: Surface Projections
         Web / Telegram presentation and interaction binding
~~~

No surface owns financial mutation policy.

## Canonical resolution command

Both Web and Telegram produce a channel-neutral command conceptually containing:

~~~text
review_item_id
household_id
actor_user_id
action
supplied_values
surface
interaction_reference
~~~

Exact Go type/name is implementation-specific.

The shared resolver:

1. locks current review_item;
2. verifies active status;
3. validates household actor;
4. reads current ReviewDecision;
5. validates allowed action;
6. validates supplied residual facts;
7. validates referenced candidates/entities against current state;
8. performs canonical mutation;
9. updates ReviewDecision/review status;
10. writes audit and human-input telemetry;
11. returns a surface-neutral resolution result.

## Projection model

review_request remains the Telegram projection record unless implementation
finds a cleaner additive projection table necessary.

Its semantics change:

> review_request represents a Telegram projection of review_item, not a
> transaction review authority.

The projection MUST be able to reference every review_item subject class without
requiring a transaction_id.

Migration strategy should be additive/safe for existing open requests.

The canonical review_item must remain resolvable even if no review_request exists.

## Projection creation

### Issue #349: honest review entry and batch notes

`OTHER_FINANCIAL_DOCUMENT` retains its classification and source evidence as
`CLASSIFIED`/`PROCESSED`. No supported extractor/resolver exists for that type,
so it does not claim `NEEDS_REVIEW` or create a financial transaction. This does
not reclassify or repair existing production records.

Accepted batch notes travel in the existing pending item, displayed confirmation
facts, proposal, and canonical transaction through both finalizers. Legacy
pending items without a note retain their prior empty-note behavior.

A canonical review producer SHOULD NOT need Telegram-specific code.

Preferred shape:

~~~text
producer writes review_item
-> projection dispatcher observes/receives review_item ID
-> eligible Telegram projection is created
-> renderer reads ReviewDecision
~~~

An implementation may synchronously enqueue projection creation in the same
transaction if correctness is preserved.

It must not duplicate review_item creation.

## Renderer registry

Telegram rendering is server-owned and exhaustive by review interaction
contract.

A renderer may be keyed by:

- interaction_mode;
- missing_facts;
- allowed_actions;
- review_type only for source-specific presentation details.

review_type alone MUST NOT override stored ReviewDecision residuals.

A coverage test must fail when a current review type can be produced but no
Telegram interaction capability exists.

## Candidate safety

### Issue #349: subject-specific transfer controls

Incoming transaction-backed relationship reviews use the transfer card in Web,
offering income, own account, household account and ignore only. Observation-
backed financial-email transfer classification remains actionable in Web; it
does not project Telegram's transaction-only callbacks. Missing transaction
subjects never receive the generic outgoing-transfer keyboard.

Categories, accounts, Wealth Accounts, duplicate candidates, and other bounded
choices are server-owned.

Telegram callback values must resolve back to currently valid household-scoped
candidates under the shared resolver.

Do not trust stale callback payloads.

Do not let the model select canonical IDs.

## Exact binding

Telegram callback/reply projection is an exact workflow binding.

The projection carries enough state to resolve review_item directly.

Open review state does not implicitly own arbitrary future chat messages.

Route-first behavior from ADR-038 remains.

### Pending correction/batch revision binding (issue #348, F03)

Pending correction and batch buttons carry a server-issued `confirmation_token`
UUID (migration `00082`), not just a yes/no choice or mutable row ID. Every
replacement and batch item edit rotates it. The executor matches that token,
household, numeric Telegram user, chat, unexpired PENDING status under the same
row lock as the mutation. Old, tokenless, resolved and replaced buttons return
the stale-action response without editing, posting or cancelling current work.
Tokens are excluded from model context and persisted tool history. A date-only
replacement also clears previously proposed category/description fields.
Existing pending prompts must be staged again after upgrade; no financial
records are automatically repaired or inferred.

## Partial resolution

### Issue #349: category/date residuals

Duplicate and transfer relationships coexist with missing category/date facts;
a relationship choice cannot erase them. The shared review boundary persists
accepted facts, proposal provenance (`date_known=true`, `date_source=USER`) and
the reduced decision atomically. It also checks persisted fallback provenance,
so an older incomplete decision cannot authorize the received-at fallback.
Expense confirmation always requires a category. Telegram refreshes the active
card after a partial answer. Transfer classification accepts a validated
`transactionAt` date through the same boundary; ignore does not require a date.

Transfer classification validates category/Wealth Account choices before
residual-fact handling. An expense choice without a category retains its HTTP
400 contract, rather than becoming a residual-facts 409. A validated partial
classification returns `ErrMissingFacts` with transaction/proposal state still
`NEEDS_REVIEW`; Telegram commits that same transaction, including the selected
type, purpose, account/category and relationship evidence. Date-only completion
confirms the persisted classification, never defaults a transfer back to income
or overwrites its accepted relationship with a generic confirm choice. It
revalidates retained categories and Wealth Account compatibility in the shared
residual boundary before final confirmation. An inactive retained choice becomes
an explicit `category` or `wealth_account` residual, never a 500 or job failure.
Both surfaces retain accepted facts while re-asking only the invalid candidate;
Web commits the validated partial classification and its audit in the same
transaction before returning 409; reloading exposes newly discovered residuals
such as a fallback-provenance date. Telegram refreshes its bound card.
Replacing an account on an open TRANSFER uses the same classifier and must
preserve its asset-purchase/investment purpose; the replacement must be active,
compatible and household-owned. No automatic candidate choice or reactivation
occurs. A legacy replacement card without a stored relationship asks for an
explicit classification; an omitted classification returns a repairable 409
with `missingFacts:["transfer_relationship"]`, not a generic 400. Nonempty
invalid classifications still fail validation. First-payslip policy choice
precedes date entry when both remain pending.

### Issue #349: aggregate lifecycle

Review completion derives source/document state from all active evidence,
transaction and proposal siblings, and open canonical reviews. Completing one
row cannot settle its unresolved siblings. All album page sources settle with
their shared document. Web and Telegram use the same aggregate; the documents
API reports that stored aggregate, including proposal-only reviews, OR an active
household-scoped linked transaction awaiting review. This includes a staged
evidence dispute's explicit `disputed_document_id` link when its evidence source
is the correction reply, not the original document. Superseded links do not
raise the flag. No backfill or production-record repair is implied.
The aggregate requires an explicit household throughout source/document/page
expansion and transaction refresh; foreign IDs or page links cannot settle a
different household's lifecycle.

A review may remain open after one residual fact is supplied.

The shared resolver returns an updated decision/residual state.

Both projections must render that updated state.

Example:

~~~text
before:
missing = [transaction_at, category]

Telegram supplies category

after:
missing = [transaction_at]
review remains OPEN
~~~

No channel may assume that one interaction always resolves the whole review.

## Cross-surface concurrency

All resolution actions lock/revalidate the canonical review.

If Web resolves first, Telegram action returns ALREADY_RESOLVED.

If Telegram recipient A resolves first, recipient B receives ALREADY_RESOLVED.

Stale actions never replay canonical mutation.

## Projection synchronization

After canonical review state changes:

- Web naturally reads the latest review_item;
- Telegram projection should be edited when practical;
- active buttons must no longer perform live mutation after resolution;
- queued sends check current review state before delivering an actionable card.

A failed message edit is operational debt, not canonical financial failure.

## Expiration

review_request expiration is projection/conversation expiration.

It must not automatically erase an unresolved canonical review_item unless a
separate product policy explicitly expires that review.

A user may reopen/reproject an active canonical review later.

## Intelligence boundary

This ADR adds no AI authority.

Review actions already represented by ReviewDecision should be handled
deterministically.

Natural-language bound replies may use existing safe interpretation where
necessary, but the model never bypasses shared resolver validation.

Do not add LLM calls just to render or route bounded callbacks.

## Telemetry

Record surface-neutral review resolution events with:

- review type;
- interaction mode;
- resolution surface;
- number/names of human-supplied fields;
- action;
- projection delivery state;
- stale attempt outcome.

Do not store sensitive raw values solely for product metrics.

### Admin operational read model

Review telemetry must be exposed through read-only Admin aggregates that follow
the existing `/api/v1/admin/*` authorization boundary.

Required aggregate capabilities:

- summary over an operational time range;
- per-review-type breakdown;
- projection/delivery diagnostics;
- safe household-level review diagnostics.

The Web Admin console consumes those aggregates through:

~~~text
/admin?tab=reviews
~~~

and compact Review health on the existing Admin Overview.

Implementation should follow existing Admin API conventions. The execution
contract defines the concrete endpoint shape.

The Admin read model MUST NOT:

- resolve or mutate review_item;
- become a second financial-review implementation;
- expose raw financial evidence merely for telemetry;
- use a model call to derive operational metrics.

Admin metrics are derived deterministically from canonical review/projection,
delivery, audit, and telemetry state.

A rollout metric is architecture-complete only when:

~~~text
event/state recorded
-> deterministic aggregate available
-> Admin surface renders it
~~~

## Consequences

Positive:

- Telegram becomes a real review inbox;
- Web and Telegram cannot drift on canonical financial rules;
- non-transaction reviews gain a clean projection path;
- future review types require explicit Telegram capability;
- stale multi-surface actions are easier to reason about.

Cost:

- review_request's transaction-era assumptions must be generalized;
- duplicated Telegram/Web resolver code must be consolidated;
- existing open review migration/backward compatibility needs care.

## Supersession

ADR-023 remains correct that review_item is universal, but "new non-transaction
references are introduced incrementally" is no longer sufficient: current review
types must reach actionable Telegram parity.

ADR-028 remains correct that Telegram does not determine canonical review
existence. Its statement that Telegram delivery is "optional" is now interpreted
as a storage/availability property, not a product UX rule. For a household with
an eligible active Telegram recipient, product policy requires an actionable
projection.

ADR-044's proposal-first rendering applies to Telegram as well as Web. The
ReviewDecision contract is channel-independent.


---

## 2026-09-28 amendment — household ownership determines projection recipients

Post-SAVR audit found a producer-level drift in email-origin reviews.

Canonical review ownership is already household-scoped. For projection,
`source_event` provenance answers where evidence came from; it does not decide
whether the owning household has an eligible Telegram recipient.

For every review producer:

```text
review_item / review_request household
-> active household_member
-> active telegram_identity
-> review_request_recipient
```

is the recipient-authority path.

An originating Telegram chat MAY be supplied as a fallback for a Telegram-origin
review when normal household recipient resolution yields no eligible recipient.
It MUST NOT be a prerequisite for projecting BANK_EMAIL, FINANCIAL_EMAIL, or any
other non-Telegram source.

Producer adapters SHOULD call the universal `ProjectReviewItem` once canonical
household ownership and an actionable review exist. They MUST NOT pre-gate that
call by searching the source payload for `message.chat.id`.

This amendment closes the S08-08 projection drift and does not change canonical
review semantics, recipient authorization, or first-valid-write rules.

## 2026-10-05 correction — source-only bank review dismissal

`UNKNOWN_BANK_TEMPLATE` cards without a transaction resolve `review:ignore`
through the shared `IgnoreBankReview` operation, also used by Web. The callback
binds the delivered chat/message, authorizes the numeric Telegram actor, and
revalidates the stored allowed action. Dismissal atomically ignores the bank
source, resolves the canonical item and all its projections/conversations, and
records the actor audit. No transaction is required, created, or deleted.
Already-resolved callbacks remain stale and never replay the mutation.

## 2026-10-08 — typed answers complete on the agent lane

From `audits/TELEGRAM-REPLY-PATH-AUDIT.md`. Typed answers to review cards
(replies and, after #301, plain answers) are owned by the conversational agent;
Go validates and commits.

- A card whose `CONFIRM` needs a typed value (merchant, date, purpose,
  correction) hands the value to extraction when Jev chooses `CONFIRM`. The rule
  is the executor's own `requiredNativeReviewDetail`, not a separate list.
- A saved purpose or correction completes the review when the card asked for
  nothing else and the transaction already satisfies the confirm rule.
- `UNKNOWN_BANK_TEMPLATE` cards without a transaction bind as `BANK_FACTS`. The
  model extracts amount and time into `COMPLETE_BANK_FACTS`; Go validates them
  and queues the shared `COMPLETE_BANK_REVIEW` job, or offers the account chooser
  when the listener has no account.
- A typed reply to a card past its 7-day projection window renews it while the
  canonical review is open. (Superseded 2026-11-05: a button tap no longer
  renews; see below.)
- `resolve_review` offers only the arguments the bound card's actions read
  (`focusReviewArguments`), for transaction and bank-fact bindings.
- A `POSSIBLE_DUPLICATE` card stores its candidates when it is sent and shows one
  merge button per candidate.
- `Process` handles button taps only; it no longer runs the bound-reply lane for
  typed text, which production always sends to `ProcessAgent`.

## 2026-10-13 — incoming rows are answerable in Telegram

An incoming screenshot row ("dana masuk") is typed `INCOME` and parked as a
`TRANSFER_CLASSIFICATION` review. Its Telegram card offered the outgoing transfer
keyboard (expense, asset purchase, own account, household, investment), and every
button reached `ClassifyTransferReview`, which only accepted `UNCLASSIFIED`
transactions, so each tap failed with "transfer review not found", retried and was
dropped with no reply; the household had to finish the review on the web.

- The keyboard follows the subject: an `INCOME` row offers **Penghasilan**
  (`review:income`, the shared `ConfirmTransactionReview`), **Rekening sendiri**,
  **Household** and **Abaikan**. Expense, asset purchase and investment contribution
  describe money leaving and are not offered.
- `ClassifyTransferReview` accepts `INCOME` for `OWN_ACCOUNT`, `HOUSEHOLD_ACCOUNT`
  and `IGNORE` only (`transferClassificationApplies`), so the web classify action
  and the typed Telegram path inherit the same rule.
- A choice that does not apply to the transaction (an old card, a stale tap) is
  answered with "Pilihan ini tidak berlaku…" and the review stays open, instead of
  failing the callback job.

## 2026-11-05 — card delivery, binding and hygiene

From the Telegram delivery audit. Canonical review semantics are unchanged; this
amendment governs only how cards reach chats and leave them.

**Recipients and threading.** Every active, authorized household member still
receives every card (household awareness; the 2026-09-28 recipient path is
unchanged and every queued send re-checks authorization). The card is threaded
only in the chat of the member who raised the review: the producer's own
message, or the review's earliest source event when it is a Telegram upload. The
other members' copies carry no `reply_to_message_id` (a message id from another
chat points at an unrelated message) and end with "Dari <nama>". A
household-level source (bank or provider email, cycle residual, wealth from
email, system) has no originating member: no thread and no line.

**Pacing.** A chat shows at most three live review cards (recipients `PENDING`
or `SENT` on an active request). A further card is rendered and stored `QUEUED`
and released oldest-first when a live card in that chat closes, expires, fails,
or is skipped. Admission, release and per-copy expiry are SQL functions under
one advisory lock per chat (migration `00094`), so the release happens in the
same transaction as the close, whichever surface closed it. Lock order: every
path takes the chat lock before it waits for a recipient row (copy expiry takes
the locks of all of its request's chats, in chat order, first; a close and a
typed-reply renewal hold the request row, then take the chat lock, then touch
recipients; a projection takes all its recipients' chat locks in chat order
before admitting any copy, whatever order the copies are presented in; nothing
waits for a request row while holding a chat lock), and a release
claims every row with `SKIP LOCKED`, so it never waits while holding the lock;
a request skipped because another transaction is closing it is released by
that close's own trigger. A member's typed reply that renews their own expired
copy is never refused for pacing.

**Expiry is per copy.** Each copy has its own seven-day window
(`review_request_recipient.card_expires_at`), starting when Telegram first
delivers that copy (its first bind; follow-up messages do not extend it), so a
copy that waited in the pacing queue or behind send retries gets its full
window, and the request's `expires_at` is extended to cover it. The worker's
maintenance tick expires due copies: the copy becomes `EXPIRED`, every message
of it loses its buttons and gains "⏳ Tombol tinjauan ini sudah kedaluwarsa.
Tinjauannya masih ada di Kotak Tinjauan.", and its slot is released, while the
other members' copies keep their own windows. The request closes as `EXPIRED`
only when no copy is live or waiting. A copy still queued when the projection
expires (by the sweep or the lazy tap path) stays queued; releasing it reopens
the request without reviving the expired copies, which stay retired and
uncounted. The `review_item` stays open on the web throughout. A tap never
renews; a typed reply to the exact card renews the projection and that
member's copy. A follow-up message delivered after its copy expired does not
revive the copy or take a pacing slot; the late message is retired.

**One live message per recipient.** `review_request_recipient.telegram_message_id`
is the recipient's current card. When a follow-up message (chooser, typed-value
prompt, re-sent card) of the same review is bound, the previous message is kept
in `review_request_message` as superseded and loses its buttons at once
("Dilanjutkan di pesan berikutnya."). Closing a request retires every message of
it, current and superseded, in every member's chat; a resolved card shows the
outcome from canonical state ("✅ Tinjauan ini sudah selesai: dicatat sebagai
Makan oleh Rina.").

**Delivery.** Telegram errors are classified: 400/403 are permanent (no blind
retry; the job is recorded `FAILED`), 429 waits Telegram's `retry_after`, and
5xx, 401/404 (token) and transport failures retry with backoff. Card sends get 8
attempts. A card that never arrives marks its recipient `FAILED` with the
redacted error in the job's final transaction, frees its pacing slot, and is
counted in the household review diagnostics; the review stays actionable in the
Review Inbox. The message id of every accepted send is recorded against its job
(`telegram_outbound_delivery`) before binding, so a retry after a failed bind
binds that message instead of sending a duplicate. A producer that reuses an
item with an existing projection reuses its cards.

**Edits.** "Message is not modified" is success. An unreachable chat is
finished. When Telegram can no longer edit a live review card, the card is sent
again with its buttons and bound (superseding the old message); a plain edit is
never replaced by an unbound, buttonless duplicate.

**Callback acknowledgement.** The API webhook answers the callback query in its
own HTTP response (`answerCallbackQuery` as the webhook reply) right after the
tap is durably captured, so the spinner never waits for the interactive lane.
The worker no longer answers callbacks.

**Races between a tap and a typed reply.** Both lanes stay as they are. Both
resolve through the guarded transition of the same `review_item` and
`review_request` rows (`status IN ('PENDING_SEND','OPEN')`), so exactly one wins.
The loser, on either lane, is answered deterministically from canonical state:
"Tinjauan ini sudah dijawab: <hasil>." A reply to a superseded message of a
review that is still open is told to continue in the newest message.

---

## 2026-11-05 amendment — one coherent, executable card

A read of the live cards found a review could take three messages (card, a new
"remember this category?" question, then a confirmation), stack two titles and two
contradictory instructions, print a placeholder merchant ("transaksi ini"), lose
its keyboard after a partial answer, and offer buttons (Abaikan on several kinds,
Benar/Ubah on income cards, any button on a source-bound transfer) that no handler
executed. The rules below replace that behaviour; they change presentation and
Telegram lanes only, never canonical mutation policy.

**Card shape.** Every card is built by one function (`buildReviewCard`) with one
title naming the missing fact, the subject facts, one line `Alasan: …` (the
decision's `whyNotAutoConfirm`, rendered in Indonesian through the ADR-054
registry), and one instruction that names only the buttons the card carries.
Producers pass facts only (`ReviewQuestion` returns `Nominal`/`Merchant` lines);
a missing merchant is omitted, never replaced by a placeholder. A projection with
no producer summary describes its subject from canonical rows
(`reviewItemSummary`: transaction, cycle residual, wealth observation,
source-bound transfer case, or screenshot row).

**Buttons follow the contract.** The keyboard is built by one function
(`reviewCardMarkup`) for the first send and for every re-render after a partial
answer, so a re-rendered card keeps its remaining actions. Actions come from the
stored `allowedActions`, falling back to the registry; Abaikan is dropped when the
contract has no `IGNORE` (a cycle residual offers `Biarkan saja` =
`LEAVE_UNALLOCATED` instead). Which kinds project at all is the registry's
`Telegram` flag.

**Every rendered button executes through the canonical resolver.**

- `review:ignore` on any review not bound to a transaction (missing-amount
  screenshot row, wealth observation, source-bound transfer, financial-email
  observation, other source/proposal/document item) calls
  `reviewdomain.IgnoreReview`, which routes to the kind's own resolver
  (`IgnoreMissingAmountProposal`, `ResolveFinancialEmailReview`,
  `ReconcileTransfer`, `DismissWealthObservation`) and otherwise parks the
  evidence like the Review Inbox. It refuses a kind whose contract has no IGNORE.
- `review:confirm` (Benar) on an income card confirms it as income through the
  canonical confirm; `review:change` (Ubah) re-renders the same card with
  Penghasilan / Rekening sendiri / Household, which the transfer classifier
  executes for an incoming transaction whatever its review kind. An income
  answer settles a stored `category` question, since income has no expense
  category.
- `review:leave` calls `ApplyCycleResidual(LEAVE_UNALLOCATED)`.
- A `TRANSFER_CLASSIFICATION` bound to a source event with an open reconciliation
  case gets `Gabung …` buttons per candidate (`review:xfer:merge:N`, the list is
  stored in `review_conversation.context_json.transfer_candidates`),
  `Catat transfer baru` (`review:xfer:new`) and Abaikan, all through
  `ReconcileTransfer`.
- A transfer card's Pengeluaran choice now shows the category chooser itself
  (it previously re-rendered the transfer keyboard).

**Category chooser.** Its first row is the merchant's own past categories
(household-scoped confirmed transactions of the same merchant; most used, then
most recent; at most three). When at least two earlier confirmed transactions of
that merchant at exactly this amount all used one category, it is marked ⭐ and
placed first. Deterministic SQL; no model.

**The tap settles the card.** A category tap edits the tapped card to
`✅ Dicatat: <kategori>` with its facts and, when the merchant is known, asks
`Pakai <kategori> juga untuk transaksi berikutnya dari <merchant>?` inline
(Ingat kategori / Hanya kali ini). The choice edits the card again to its final
text with no buttons. No further message is sent. Because the confirm resolves
the request in the same transaction, the 00079 trigger has already queued a
retirement for that card; it is marked superseded and the outcome edit retires
the card instead, so an unordered retire job cannot strip the inline choice.
Other recipients' cards retire normally. A *typed* confirmation still asks the
question as its own bound message.
Each settle of a card bumps a per-card outcome revision in
`review_conversation.context_json.card_outcomes`, and the edit job carries it
(`outcome_request_id`, `outcome_revision`). The worker applies an outcome edit
only while it is the newest revision, holding the conversation row lock across
the check and the Telegram call (settles take the same lock), so a delayed,
retried or out-of-order earlier edit is dropped and cannot restore answered
buttons.
Outcome edits enter through the delivery path's `DeliverEdit` (authorization,
then the revision guard); an expired or superseded copy (delivery rules above)
receives the outcome text without buttons.
A card asking the remember question counts as live for stale taps even though its
request is resolved, so a duplicate tap never retires it. Generic button
retirement never touches a card that has an outcome, and a retirement queued
before the first outcome carries revision 0 and is dropped once an outcome
exists.
The question is not asked when the same rule already exists, nor when the
merchant was already confirmed with a different category: a single
auto-applied default would miscategorize that merchant's other purchases, and
the chooser's history row already answers it in one tap.

**Stale taps.** A stale tap on a card whose review is no longer live loses its
buttons (as the generic stale path already did); a rejected choice on a card
whose review is still open keeps them. A cycle residual refreshed by new
transactions is re-rendered as the same card with the new residual, not a
generic sentence.

**Reopened reviews.** `ReopenTransactionReview` (only caller: Unmerge) stores a
fresh `POSSIBLE_DUPLICATE` decision from the registry, since reversing a merge is
what reopens the duplicate question. It never copies the previous snapshot: the
other residuals are recomputed from canonical state (an expense with no category,
a received-time date fallback, an unclassified row), so an answered fact is not
asked again; the previous kind is kept as provenance. It enqueues
`PROJECT_TELEGRAM_REVIEW`, and the worker projects it through `ProjectReviewItem`,
which is idempotent per item.

**Typed payslip dates.** An explicit date reply ("25 Okt", "25/10", "25/10/2026",
"2026-10-25", "kemarin", "hari ini") is parsed in Go; only a reply the parser
cannot read goes to the model.

