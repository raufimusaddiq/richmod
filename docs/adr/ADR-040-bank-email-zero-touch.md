# ADR-040: Bank email zero-touch classification

## Status

Accepted for implementation — 2026-09-23. Implements PRD §9 (Stage 3). Amended by ADR-045 on 2026-09-24.

## Context

Bank notifications are the lowest-friction source Richmod has: the evidence
arrives by itself with amount, time, direction, and channel. Two things still
forced a human into that path.

1. A new merchant with no learned category always opened a review, even when the
   transaction was otherwise complete.
2. A single bounded verification ruling could park an ordinary transaction. Jev
   is probabilistic: on an identical Jago body the payment-mechanism predicate
   (then `channel_supported`, since replaced by `semantic_grounded` in SAVR-06)
   flipped roughly one run in five, and one negative ruling was fatal, so a
   completed debit-card purchase became `UNKNOWN_BANK_TEMPLATE`.

Lowering thresholds would have hidden both symptoms while accepting worse
decisions, which the PRD forbids.

## Decision

Two changes to the frozen `SPENDING_ONLY` policy path, neither touching the
deterministic bank policy or its evidence semantics.

**Category classification.** When the deterministic policy decides an expense
whose merchant has no learned category, the bounded plane chooses one category
from the household's own active set. A decisive, well-separated answer
auto-confirms with zero user input. An undecided answer still parks a review, but
it is category-only: amount, time, direction, and channel are known facts and are
not requested again.

**Bounded verification retry.** A provider failure (timeout, gateway failure,
rate limit, malformed response) is retried once. A semantic negative is a verdict,
not a failure, and immediately sends the email to review. Retrying a negative
would let either of two draws approve and raise the effective acceptance rate,
so it is not the safe retry allowed by PRD §3.7.

The bounded candidate set is server-owned — category slugs the household already
has — and Go maps the approved slug back to the canonical category ID. Jev never
chooses a hidden database identifier, and nothing here mutates the ledger
directly.

## Amendment — merchant absence does not preempt category intelligence (2026-09-24)

Merchant is optional enrichment, so an otherwise valid merchant-like expense
with merchant = NULL must still receive the bounded category opportunity before
human review. The bounded question may use the remaining evidence/state without
fabricating a merchant.

Required behavior:

~~~text
valid amount/date/direction/channel
merchant = NULL
category unresolved
-> bounded category decision
-> decisive: CONFIRMED with merchant NULL
-> undecided/failure: category-only review
~~~

This amendment does not remove independent bank evidence verification. Evidence
support and category choice are distinct semantic questions and may both be
required when they protect different correctness properties.

## Consequences

### Amendment — channel-independent merchant-first review (2026-09-30)

`UNKNOWN_MERCHANT` reviews collect the merchant name before category selection,
regardless of payment channel or producer. Their stored review decision names
`merchant` and `category`; Telegram starts in `AWAITING_MERCHANT`. Bank email
does not attempt category inference while the policy is `UNKNOWN_MERCHANT`.
This supersedes the merchant-optional category opportunity above. Reviews with
a known merchant (`AMBIGUOUS_CATEGORY`) retain bounded category inference.

An exactly bound free-text reply matching a household-confirmed, auto-applicable
merchant alias reuses that alias's active category without a model call or a
second category question. Unknown names are saved through the active agent review
path, then category selection remains explicit. Saving the merchant updates the
review decision so later category confirmation does not re-request it. Ambiguous,
inactive, unconfirmed, or cross-household aliases never authorize confirmation.

Web Review Inbox confirmation also accepts a merchant name without a category
when the same exact household-confirmed alias resolves an active category. Its
category picker allows this attempt when merchant is missing; unmatched names
produce a category-specific error and focus the now-required category picker.
Changing the merchant allows recall again. An explicitly selected category wins. Other
missing facts still block confirmation. Ingestion auto-confirm policies and
Financial Provider Email resolution are unchanged.

- A decisive category requires zero human input (PRD §9 exit criterion).
- An ordinary new-merchant expense no longer becomes a review unless the
  category decision is genuinely undecided.
- Category reviews carry `missing_facts: ["category"]`; unknown-merchant
  reviews carry `["merchant", "category"]`. Known facts are never re-requested.
- Telegram category callbacks honor that stored contract even when merchant is
  NULL. Receipt duplicate reviews expose candidate merge, confirm-as-new, and
  ignore choices; transfer-only actions remain unavailable there.
- The merchant prompt keeps its `Beli aset` and `Abaikan` buttons (ADR-036),
  matching the category chooser it replaced, so a missing-merchant card can
  still be reclassified as an asset purchase. A reply still binds to the
  merchant field unless the asset button is tapped.
- Rare flaky negatives cost one extra bounded call instead of a review.
- Provider-specific behaviour is unchanged and still absent: classification is
  generic over the household category set.
