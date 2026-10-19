# ADR-042: Per-row screenshot auto-confirm with one bounded ruling per image

## Status

Accepted for implementation — 2026-09-23. Implements PRD §11 (Stage 5). Amended by ADR-045 on 2026-09-24.

## Context

A transaction screenshot may contain many independent financial events, and the
unit of certainty is the row, not the image. The previous path treated every
unmatched row as ambiguous: it created a `NEEDS_REVIEW` transaction and a review
per row. An unmatched row usually means "new transaction", not "ambiguous
transaction" (PRD §11.1), and ten review cards for one image violates the
minimal-interaction contract (PRD §11.4).

## Decision

After the image's single generative extraction, Go validates every unmatched
row and partitions it by residual uncertainty. A row whose constrained category
already passes the screenshot source-acceptance contract does not enter Jev.
Only rows whose bounded category remains unresolved are included in one batched
bounded request using the household's own category slugs as the possibility
space (PRD §11.3, as amended by ADR-045).

A row auto-confirms only when all hold (PRD §17, §11.2):

- it matched no existing transaction;
- it is an OUT/expense row;
- a valid category is available from the accepted constrained vision result or from a decisive residual bounded rescue;
- no unresolved category conflict remains between source interpretation and any residual bounded rescue;
- the screenshot printed a transaction date;
- extraction confidence is at least 0.90.

The confirmed path writes an accepted proposal, a `CONFIRMED` expense, its
evidence row, and an `AUTO_CONFIRM_SCREENSHOT_ROW` audit entry. Rows resolved by
the residual Jev batch may share one bounded-decision provenance record; rows
that never entered Jev retain generative/deterministic provenance and MUST NOT
claim Jev participation. One row keeps its own audit entry.

Rows that stay unresolved keep the review path and store the PRD §7
ReviewDecision so the Inbox asks only about the dimension that is genuinely
unresolved: `category` for an undecided expense, `transfer_relationship` for an
incoming row, `duplicate_relationship` for a possible duplicate. Incoming rows
never auto-confirm — evidence still cannot separate income from an own-account or
household transfer (PRD §11.5) — and the previous approval flow is unchanged.

The document also enqueues one batch summary in the spirit of PRD §11.4
("`N transaksi ditemukan`", recorded, linked, and how many still need a decision)
instead of one chat message per row.

A nil/unavailable bounded plane disables only residual bounded rescue. Rows
whose source-acceptance contract already passes may still follow their direct
canonical path; rows that actually require bounded rescue remain reviewable and
must not be guessed.

## Amendment — selective residual batch (2026-09-24)

The bounded batch is a rescue queue, not a second-pass queue.

Example:

~~~text
20 extracted rows
17 categories accepted by source policy
3 categories unresolved

Jev questions = 3
not 20
~~~

Already-clear rows must not claim Jev provenance. Mixed batches may therefore
contain direct generative rows and Jev-rescued rows in one document.

## Amendment — screenshot event identity (2026-10-14)

Amount, merchant and temporal proximity nominate a candidate; they do not prove
that two payment screenshots represent one event. The generic extractor retains
printed transaction, reference and order identifiers as bounded untrusted facts.
Go validates their shape and compares them only against household-scoped,
amount/type-compatible candidates and their active screenshot evidence.

Conflicting identifiers prevent an automatic link. Matching identifiers preserve
repeated-screenshot deduplication. Without comparable identifiers, conflicting
printed precise times remain duplicate ambiguity for human review rather than
an automatic merge. Receipt and bank-email matching policies are unchanged.
No provider-specific parser or production-history repair is introduced.

## Amendment - multipage extraction (2026-10-15, issue #348 F01)

Screenshot extraction reads all stored document pages in page-index order using
the same loader as receipts, with the legacy primary-attachment fallback only
when no page rows exist. All pages reach the single extraction/repair input;
an unreadable page fails processing rather than silently extracting a subset.
Rows retain their existing document/proposal/evidence linkage and row indices.

## Amendment - positive identity before auto-link (2026-10-15, issue #348 F02)

Automatic screenshot evidence linking requires matching comparable observed
event identifiers with no contradictory/invalid bound evidence. Amount, merchant,
date-only timestamps, and even identical precise timestamps alone do not prove
identity: their candidate scores are capped at .80 and remain duplicate review.
Matching identifiers still allow repeated-evidence linking; conflicting
identifiers still exclude distinct events. Receipt/bank matching is unchanged.

## Consequences

- Clear rows reach canonical state with no user interaction; only genuinely
  uncertain rows ask a question (PRD §11 exit criterion).
- A many-row screenshot produces one summary plus one question per uncertain row,
  not one card per row.
- Thresholds match the bank-email and Telegram category policies (MinTop .85 /
  MinMargin .20); no threshold was relaxed to reduce reviews.
- No provider-specific branch, no new table, no new dependency, and no extra
  bounded call per row.


## Amendment: reprocess never auto-confirms past a prior match (#363)

Auto-confirm assumes "the matcher found nothing" means "new transaction". After
a reprocess that is not true: a source event whose earlier proposals (any
status) carry `matched_transaction_id` was already linked to a real
transaction, and amount drift or the 72-hour window can hide it from today's
matcher. `persistScreenshot` therefore adds those household-owned transactions
as candidates of every unmatched row, so the row goes to `POSSIBLE_DUPLICATE`
review instead of writing a second CONFIRMED row. Earlier proposals stay as
history; a reprocess takes the next free `proposal_key` (`row-001-r2`) instead of
colliding with the `(source_event_id, proposal_key)` unique constraint. No
schema change: proposal metadata already retains the match.

The prior matches (`matched_transaction_id`, or `merged_into` after a duplicate
merge; non-UUID values ignored, VOIDED rows skipped) are stored in the review
decision as `knownFacts.prior_match_transaction_ids`. `duplicateCandidateRows`,
shared by the Telegram card and the conversational flow, offers them first even
when amount or window no longer match. Every unmatched row of such a document is
sent to review, which can over-block an unrelated new row; that is accepted
because a prior proposal cannot say which row it belonged to.
