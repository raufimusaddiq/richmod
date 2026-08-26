# ADR-023: Universal review items

## Status

Accepted — V3 P0.

## Decision

Represent every human review opportunity with a `review_item` row. A review
item belongs to one household and may point to a transaction, proposal, source
event, or document. The existing `review_request` remains the Telegram delivery
and conversation adapter and links one-to-one to the universal item.

## Consequences

- Ambiguity can be persisted before a canonical transaction exists.
- Web and Telegram clients can resolve the same underlying review item.
- Existing Telegram review code remains compatible while new review types and
  non-transaction references are introduced incrementally.
