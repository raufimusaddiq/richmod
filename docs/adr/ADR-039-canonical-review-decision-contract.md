# ADR-039: Canonical ReviewDecision contract

## Status

Accepted for implementation — 2026-09-23. Implements PRD §7 (review contract).

## Context

Richmod already stores canonical review work in `review_item`, but the record
only says *what type* of review it is (`review_type`). The semantic detail that
explains *why the review exists* is reconstructed separately in the API, in the
Telegram renderer, and in the web Inbox, each with its own rules. They drift.

The PRD requires every review to state: what Richmod knows, what it proposes,
what is unresolved, why auto-confirm was blocked, and exactly what the user must
decide. That is also what the later stages need — proposal-first Inbox (Stage 7),
category-only bank review (Stage 3), and the RHICE telemetry numerator (Stage 0
exit criterion).

## Decision

Add one nullable `jsonb` column, `review_item.decision`, holding the PRD §7
ReviewDecision contract. It is populated by the Go path that creates the review,
using the shared `internal/reviewdec.Decision` type. The column is additive and
nullable; existing open reviews remain resolvable and are never backfilled with
invented facts.

The contract:

```text
version, subject{type,id}, source_event_id, reason_code, decision_class,
known_facts, proposed_facts, missing_facts, conflicting_facts, bounded_choices,
evidence_refs, decision_source, decision_policy_version, decision_provenance,
why_not_auto_confirm, allowed_actions, interaction_mode
```

Bounded choices and canonical IDs stay server-side. The model never sees or picks
a canonical identifier, and the contract grants no mutation authority — Go still
validates every resolution against current state.

Stage 2 deliberately does **not** change confirmation policy. Reviews that exist
today still exist; they simply now carry their own explanation. Behavior changes
arrive in Stages 3–7.

## Consequences

- The Inbox, Telegram, and future UIs render from one stored contract instead of
  re-deriving the reason.
- RHICE becomes computable from stored decisions rather than inferred from
  action names (Stage 0 exit criterion closes when this lands).
- One jsonb column and one Go type; no new table, service, or pipeline.
- Nullable column keeps backward compatibility with pending reviews and with
  every review-creating path not yet migrated in this stage.
