# ADR-041: Receipt auto-confirm for clear new receipts

## Status

Accepted for implementation — 2026-09-23. Implements PRD §10 (Stage 4). Amended by ADR-045 on 2026-09-24.

## Context

Receipts arrive with the full evidence set: total, a parseable date, merchant
when printed, and itemisation that can be checked arithmetically. The existing
path only confirmed a receipt when it matched one strong existing transaction.
A perfectly clear new receipt — no match to link, category resolved, arithmetic
consistent — still opened a review, which is the "NEEDS_REVIEW because no
existing match exists" behaviour PRD example D names as wrong.

## Decision

Before opening a receipt review, confirm a new receipt directly when all of the
following hold:

- no candidate transaction matched at all (so there is no duplicate ambiguity to
  resolve);
- a household category was resolved for it;
- the receipt itself printed a transaction date (`DateKnown`);
- extraction confidence is at least 0.90;
- arithmetic is either unavailable or consistent.

The confirmed path writes the same proposal, transaction, evidence row, and
document state as the review path, but with `CONFIRMED` status and an
`AUTO_CONFIRM_RECEIPT` audit entry. Everything else is unchanged: a receipt with
candidate matches still links evidence or opens a `POSSIBLE_DUPLICATE` review,
and a receipt missing its date or category still goes to review asking only for
the unresolved fact.

Duplicate safety is preserved by construction — the auto-confirm branch is only
reachable when no candidate matched, so it cannot race an existing transaction.
To make that guarantee real, the match query keeps every same-amount, same-
direction transaction inside the window, including low-scoring ones whose
merchant text differs; a filtered-out weak match must still force the duplicate
review instead of disappearing from the gate (Hermes review on PR #127).

## Amendment — no mandatory Jev replay after clear vision extraction (2026-09-24)

ADR-045 makes the receipt path single-pass on the clear happy path.

A constrained receipt category produced by required vision extraction may be
consumed directly when the complete receipt source-acceptance contract passes.
Extraction confidence is not sufficient by itself; Go must still validate the
required typed fields, printed/acceptable date, amount, arithmetic where
available, category membership, duplicate safety, and absence of material
conflict.

Jev is added only when a bounded fact remains unresolved. The primary new rescue
case is category:

~~~text
vision extraction
-> category unresolved
-> one bounded category Jev rescue
-> decisive: continue with zero human input
-> undecided/failure: category-only review
~~~

A receipt whose category is already accepted must not receive a Jev category
call merely to confirm the same label. A receipt with genuinely missing date
must ask for the date rather than spend model calls guessing it.

## Consequences

- A clear new receipt reaches canonical state with no user interaction (PRD §10
  exit criterion, R1).
- Duplicate ambiguity remains guarded (R2, R3).
- Reviews that remain still request only the unresolved facts (R4, R5).
- A receipt with no printed date is never confirmed against its upload time, so
  a fallback timestamp cannot masquerade as the receipt's own transaction time
  (PRD §18.4); such a receipt still asks only for the date.
- No new table, service, or dependency.
