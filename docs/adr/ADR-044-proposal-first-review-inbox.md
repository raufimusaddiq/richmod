# ADR-044: Proposal-first Review Inbox

## Status

Accepted for implementation — 2026-09-24. Implements PRD §13-§14 (Stage 7).

## Context

The universal Review Inbox rendered a full edit form for every card. The form
was the interaction, so a review that already knew the amount, the merchant and
the category still opened as four empty inputs. That contradicts the proposal-first
model (PRD §13.1) and hides the one fact the review is actually blocked on. The
canonical ReviewDecision contract (ADR-039) already stores `proposedFacts`,
`missingFacts`, `whyNotAutoConfirm` and `allowedActions` — the Inbox was simply
not reading them.

## Decision

The Inbox renders from the stored decision, in this order:

1. what happened (canonical facts: amount, time, source);
2. what Richmod proposes (`decision.proposedFacts`);
3. why the user is needed (`decision.whyNotAutoConfirm`);
4. one primary accept action, with full editing behind an explicit edit step
   (PRD §13.3).

A required input is rendered only for a dimension the decision named in
`missingFacts` (PRD §13.4). A known financial entity — a resolved account, a
resolved Wealth Account — is shown, never re-asked (PRD §3.3). Evidence moves
behind a secondary disclosure (PRD §13.6). The optional note stays optional
(PRD §13.5).

The API exposes `proposedFacts` and `missingFacts` on the review item, so the
client has no independent notion of what is missing: it renders the decision it
was given and nothing it inferred.

## Consequences

- A card asks at most for what blocks it, so resolving is one click in the
  common case.
- Cards for reviews created before the decision contract existed have no
  `missingFacts`; they keep rendering the full form and stay resolvable.
- No new table, migration, endpoint or dependency — the decision contract was
  already in place; only its consumer changed.

