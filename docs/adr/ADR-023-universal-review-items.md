# ADR-023: Universal review items

## Status

Accepted — V3 P0. Amended by ADR-046 on 2026-09-25.

## Decision

Represent every human review opportunity with a `review_item` row. A review
item belongs to one household and may point to a transaction, proposal, source
event, document, or later-added canonical review subject.

`review_item` is the product's canonical Review Inbox backlog.

The existing `review_request` remains a Telegram delivery/conversation
projection and links to the universal item. ADR-046 clarifies that the projection
must no longer depend on a transaction-only subject model.

For a household with an eligible active Telegram recipient, current producible
review types require an actionable Telegram projection. Canonical review
existence remains independent of Telegram availability.

## Consequences

- Ambiguity can be persisted before a canonical transaction exists.
- Web and Telegram resolve the same underlying review item.
- Channel projections do not own canonical financial policy.
- New review types must declare/test Telegram interaction capability instead of
  being introduced as silent Web-only backlog.
- Existing transaction-era review_request rows remain backward-compatible during
  the ADR-046 migration.
