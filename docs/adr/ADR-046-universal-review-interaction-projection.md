# ADR-046: Universal review interaction projections

## Status

Accepted architecture direction — 2026-09-25.

Amends:

- ADR-023 Universal review items;
- ADR-028 Canonical universal review orchestration;
- ADR-044 Proposal-first Review Inbox.

Related:

- docs/RICHMOD_UNIVERSAL_REVIEW_INTERACTION_PRD.md
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

## Partial resolution

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
