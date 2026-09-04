# ADR-028: Canonical universal review orchestration

## Status

Accepted — correctness/performance remediation Release 2.

## Decision

`review_item` is the canonical household backlog for transactions, proposals,
source events, and documents. `review_request` is only an optional Telegram
delivery projection. Active-subject uniqueness is enforced in PostgreSQL, and
resolution metadata is retained.

Incomplete or low-confidence bank extraction creates a source review without a
transaction. A first valid payslip creates a proposal-backed
`PAYSLIP_CONFIRMATION` review and no income; a missing pay date creates
`MISSING_PAY_DATE`. Telegram delivery is optional and never determines whether
the canonical review exists.

## Consequences

- Ambiguous evidence is visible on the web even without a Telegram identity.
- One subject cannot appear multiple times in the active backlog.
- No first-payslip income exists before the household chooses its meaning.
