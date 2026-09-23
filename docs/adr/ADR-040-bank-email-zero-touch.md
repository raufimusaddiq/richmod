# ADR-040: Bank email zero-touch classification

## Status

Accepted for implementation — 2026-09-23. Implements PRD §9 (Stage 3).

## Context

Bank notifications are the lowest-friction source Richmod has: the evidence
arrives by itself with amount, time, direction, and channel. Two things still
forced a human into that path.

1. A new merchant with no learned category always opened a review, even when the
   transaction was otherwise complete.
2. A single bounded verification ruling could park an ordinary transaction. Jev
   is probabilistic: on an identical Jago body `channel_supported` flipped
   roughly one run in five, and one negative ruling was fatal, so a completed
   debit-card purchase became `UNKNOWN_BANK_TEMPLATE`.

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

## Consequences

- A decisive category requires zero human input (PRD §9 exit criterion).
- An ordinary new-merchant expense no longer becomes a review unless the
  category decision is genuinely undecided.
- Reviews that do exist carry the PRD §7 contract with `missing_facts:
  ["category"]`, so the Inbox cannot ask for facts Richmod already knows.
- Rare flaky negatives cost one extra bounded call instead of a review.
- Provider-specific behaviour is unchanged and still absent: classification is
  generic over the household category set.
