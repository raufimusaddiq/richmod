# ADR-028: Canonical universal review orchestration

## Status

Accepted — correctness/performance remediation Release 2. Amended by ADR-046 on
2026-09-25.

## Decision

`review_item` is the canonical household backlog for transactions, proposals,
source events, documents, Wealth observations, financial-email observations, and
other supported review subjects.

`review_request` is a Telegram delivery/conversation projection. It is optional
for canonical correctness and availability: a review exists even when Telegram
is not linked or delivery fails.

ADR-046 adds a product requirement to that architectural statement:

> when an eligible active Telegram recipient exists, every current producible
> human review must have an actionable Telegram projection.

"Optional" therefore does NOT mean that Richmod may intentionally make an
ordinary review Web-only for a Telegram household.

Active-subject uniqueness is enforced in PostgreSQL, and resolution metadata is
retained.

Incomplete or low-confidence bank extraction may create a source review without
a transaction. A first valid payslip may create a proposal-backed review.
Telegram projection must support those non-transaction subjects without
inventing a transaction.

## Consequences

- Ambiguous evidence remains visible on Web even without Telegram.
- Telegram failure never prevents creation of canonical review work.
- One subject cannot appear multiple times in the active backlog.
- Web and Telegram use the same canonical review resolution rules.
- Cross-surface and multi-recipient races are resolved by locking/revalidating
  review_item; first valid resolution wins.
