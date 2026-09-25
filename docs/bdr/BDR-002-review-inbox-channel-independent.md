# BDR-002: Review Inbox is a channel-independent decision backlog

## Record type

Business Decision Record.

## Status

Accepted product decision — 2026-09-25.

## Decision owner

Product Design.

## Related documents

- docs/RICHMOD_UNIVERSAL_REVIEW_INTERACTION_PRD.md
- docs/adr/ADR-046-universal-review-interaction-projection.md
- docs/RICHMOD_INTELLIGENCE_ROUTING_MINIMAL_INTERACTION_PRD.md
- docs/adr/ADR-039-canonical-review-decision-contract.md

## Business problem

Richmod has invested in Telegram as an intelligent conversational finance
interface, but the review experience still partially treats Web as the real
product and Telegram as a notification shortcut.

That creates avoidable friction exactly where PRD #144 says human attention is
scarce.

A user can provide a transaction, document, or question in Telegram, let Richmod
perform extraction and bounded reasoning, then be forced to open a different
surface for the one remaining human decision.

The problem is not lack of intelligence.

It is interaction fragmentation.

## Decision

The product defines Review Inbox as the canonical review backlog, not as the Web
page that visualizes it.

~~~text
review_item = product inbox
Web         = rich projection
Telegram    = fast actionable projection
~~~

For households with active Telegram identities, every current human review type
must have an actionable Telegram path to resolve the ordinary blocker.

Web remains optional for richer detail and bulk inspection.

## Why this follows the previous product decision

BDR-001 prioritized:

1. correctness;
2. minimum human interaction;
3. minimum machine work.

BDR-002 adds:

4. do not make an irreducible human interaction more expensive by forcing an
   unnecessary channel switch.

A Web redirect is not free.

It adds:

- app/browser switch;
- authentication/session dependency;
- navigation;
- locating the same review;
- repeating context.

If Telegram can safely represent the decision, that cost has no product value.

## Accepted interaction model

~~~text
canonical review
      |
      +-> Telegram: fastest decision path
      |
      +-> Web: richest inspection path
~~~

The two surfaces are equal clients of the same review domain.

They are not two financial authorities.

## Product requirement

For a current supported review type:

~~~text
active review
+ eligible Telegram recipient
=> actionable Telegram projection
~~~

"Actionable" means the normal blocking decision can be completed there.

A notification with a Web redirect does not count.

## Alternative A — Web is canonical UX, Telegram only notifies

Rejected.

Benefits:

- simplest implementation;
- avoids extending Telegram callbacks.

Costs:

- contradicts Telegram's first-class product role;
- adds avoidable channel switching;
- wastes ReviewDecision structure;
- makes the intelligent Telegram workflow feel incomplete;
- systematically disadvantages users who intentionally avoid Web.

## Alternative B — implement every Telegram review independently

Rejected.

Benefits:

- incremental;
- low initial refactor cost.

Costs:

- duplicates financial mutation rules;
- Web and Telegram drift;
- one surface can accept actions the other rejects;
- every future review type needs two business implementations;
- stale review races become harder to reason about.

## Alternative C — shared canonical resolver + channel projections

Accepted.

Benefits:

- one financial review authority;
- consistent ReviewDecision enforcement;
- Telegram can reach full coverage;
- Web remains rich without being mandatory;
- new channels can reuse the same resolver later;
- easier concurrency and stale-action handling.

Trade-off:

- requires extracting transaction-specific Telegram resolution logic into a
  channel-neutral Go domain boundary;
- review_request schema/lifecycle may require migration away from its historical
  transaction-only assumptions.

The refactor is justified because it removes an architectural source of product
drift.

## Surface responsibilities

### Canonical review domain

Owns:

- authorization;
- current review state;
- allowed actions;
- residual validation;
- canonical mutation;
- concurrency;
- audit;
- human-input telemetry.

### Telegram

Owns:

- delivery;
- compact rendering;
- callbacks;
- bound replies;
- candidate pagination;
- resolved/stale message state.

### Web

Owns:

- rich rendering;
- evidence inspection;
- full editing where allowed;
- bulk review navigation.

## Exceptions

Current review types have no pre-approved Web-only exception.

A future Web-only exception requires an explicit product decision proving that
Telegram cannot safely represent the minimum decision.

"Engineering work is not done" is not a product exception.

## Product metrics

Primary new metric:

**Telegram Actionable Review Coverage**

Target: 100% for eligible current reviews after rollout.

Supporting metrics:

- Web Escape Rate -> 0%;
- Review Completion Surface;
- Review Interaction Turns;
- Cross-Surface Stale Action Rate;
- Telegram delivery success/retry.

Existing RHICE and correction-rate guardrails remain authoritative.

## Success definition

A user who chooses Telegram as their daily Richmod interface should be able to
go through ordinary finance capture, follow-up, review, and correction without
being forced into Web.

Web should feel like an optional control center, not a mandatory second half of
the product.
