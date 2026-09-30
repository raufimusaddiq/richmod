# ADR-056: Contract-complete cycle-review UI surfaces

## Status

Accepted.

## Context

Sprint 3 rebuilds `/analytics` around the [cycle-review PRD](RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md).
The existing page requested five aggregate endpoints plus daily history and
derived its KPIs in the browser. The PRD requires one deterministic contract,
explicit baselines, and drill-down that cannot silently widen the period.

Three review paths would otherwise render from shapes the backend does not
actually send: a persisted household decision list, a comment thread, and
monthly `categoryId`/year-month transaction filters. Building them in Sprint 3
would place a non-functional decision log in the release and weaken the drift
guards.

## Decision

A surface ships only against a contract the backend already serves. The
deterministic cycle-review endpoint and the transaction list endpoint are the
authoritative contracts for this sprint.

- The review consumes one `cycle-review-v1` response. It never derives
  authoritative totals, medians, shares or significance in JavaScript.
- Drill-down uses only filters that provably preserve the selected cycle:
  `from`/`to` inclusive translation of the facts API's exclusive cutoff,
  `status=CONFIRMED`, `type=SPENDING`, plus exact `categoryId`,
  `merchantId` or `transactionId`.
- Commentary is optional, explicitly generated, and isolated from deterministic
  facts. Historical advice rows are excluded from the current review view.

## Deferred

| Surface | Reason | Owner |
| --- | --- | --- |
| Closed-cycle meeting mode with progress state | Needs the decision record to persist, not only to navigate | Sprint 4 |
| Household decision create/read | Requires a household-scoped audited entity, migration and schema docs | Sprint 4 |
| Commentary thread, author, rating and comments | No persisted contract; automatic prose is not a tool result | Not planned |
| Per-month transaction filters used by the retired calendar mode | Month keys cannot be accepted unchanged by the ledger's exclusive date filters | Unrequested |

## Consequences

The page cannot display a fact the API does not compute, and Sprint 4 gets a
stable deterministic review to attach persisted decisions to. Sprint 3 does not
add schema, migrations, production DML, provider access or new infrastructure.
Revisit this ADR if a later sprint needs a surface that genuinely requires a new
persisted contract.
