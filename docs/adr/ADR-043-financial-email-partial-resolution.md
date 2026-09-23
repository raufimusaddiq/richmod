# ADR-043: Partial financial-email entity resolution

## Status

Accepted for implementation — 2026-09-24. Implements PRD §12 (Stage 6).

## Context

Financial provider email resolves its funding account and its provider Wealth
Account independently. The evidence frequently settles one and not the other:
an email can name the bank it paid into while its provider holding stays
ambiguous. The review previously required both entities in one submission, so a
card that already knew "Bank Jago" still asked the household to choose it again —
exactly the re-asking PRD §3.3 forbids.

## Decision

`SET_FINANCIAL_EMAIL_ENTITIES` now accepts a partial submission:

- the resolved entity persists on the observation (`resolved_account_id`,
  `resolved_wealth_account_id`) and is not requested again;
- the review stores a PRD §7 decision whose `missingFacts` names only the
  unresolved dimensions (`funding_account`, `wealth_account`);
- the API merges persisted known facts with the newly supplied values, then
  validates the complete result. An entity that is still unresolved at decision
  time must be supplied — a resolution may not leave a half-bound observation;
- the Inbox renders known entities as known and asks only for the missing one.

Two safety rules were added where partial resolution exposed them:

- a `USER`-sourced entity alias is never overwritten by review learning
  (PRD §19, "learning != silent assumption");
- an alias is re-learned only for an entity this review newly resolved, so a
  partial submission cannot rewrite an existing mapping.

## Consequences

- The card asks one question instead of two, and never re-asks a known entity.
- Resolution still runs through the same deterministic worker; the review only
  supplies identity, never canonical accounting semantics.
- Reviews created before the decision contract existed have no `missingFacts`
  and keep asking for both entities, so they stay resolvable.
- No new table, migration, or dependency.

