# ADR-044: Proposal-first Review Inbox

## Status

Accepted for implementation — 2026-09-24. Implements PRD §13-§14 (Stage 7).
Amended by ADR-046 on 2026-09-25 to make proposal-first rendering
channel-independent.

## Context

The universal Review Inbox rendered a full edit form for every card. The form
was the interaction, so a review that already knew the amount, the merchant and
the category still opened as four empty inputs. That contradicts the proposal-first
model and hides the one fact the review is actually blocked on.

The canonical ReviewDecision contract stores proposedFacts, missingFacts,
whyNotAutoConfirm, allowedActions, and interactionMode.

## Decision

Every review projection, including Web and Telegram, renders from the stored
decision in this order:

1. what happened / known facts;
2. what Richmod proposes;
3. why the user is needed;
4. the minimum action or input required.

A required input is rendered only for a dimension named in missingFacts.

Known financial entities are shown, never re-asked.

Web may expose full editing/evidence behind secondary controls.

Telegram uses bounded buttons, paged server-owned choices, or exact bound replies
to collect the same residual decision. A Web "View details" action is optional;
it is not the required completion path for current supported review types.

The surface must not derive a different missing field from review_type when a
stored ReviewDecision exists.

## Consequences

- Common reviews remain one-click/one-reply decisions.
- ReviewDecision becomes a true channel-independent presentation contract.
- Web is the richest inspection surface, not the canonical human-decision
  authority.
- Telegram can complete the same ordinary blocker through the same shared Go
  resolver.
- Legacy reviews without a decision require a compatibility renderer, but may
  not silently become permanently Web-only after the universal-review sprint.
