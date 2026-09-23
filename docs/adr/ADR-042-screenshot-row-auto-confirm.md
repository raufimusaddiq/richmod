# ADR-042: Per-row screenshot auto-confirm with one bounded ruling per image

## Status

Accepted for implementation — 2026-09-23. Implements PRD §11 (Stage 5).

## Context

A transaction screenshot may contain many independent financial events, and the
unit of certainty is the row, not the image. The previous path treated every
unmatched row as ambiguous: it created a `NEEDS_REVIEW` transaction and a review
per row. An unmatched row usually means "new transaction", not "ambiguous
transaction" (PRD §11.1), and ten review cards for one image violates the
minimal-interaction contract (PRD §11.4).

## Decision

For every unmatched row, one batched bounded request rules on the row's category
using the household's own category slugs as the possibility space (PRD §11.3).
A row auto-confirms only when all hold (PRD §17, §11.2):

- it matched no existing transaction;
- it is an OUT/expense row;
- the bounded plane reached a decisive, well-separated category;
- the image and the bounded plane did not name different categories;
- the screenshot printed a transaction date;
- extraction confidence is at least 0.90.

The confirmed path writes an accepted proposal, a `CONFIRMED` expense, its
evidence row, and an `AUTO_CONFIRM_SCREENSHOT_ROW` audit entry. Rows of one image
share one `judgment_decision` provenance row; one row keeps its own audit entry.

Rows that stay unresolved keep the review path and store the PRD §7
ReviewDecision so the Inbox asks only about the dimension that is genuinely
unresolved: `category` for an undecided expense, `transfer_relationship` for an
incoming row, `duplicate_relationship` for a possible duplicate. Incoming rows
never auto-confirm — evidence still cannot separate income from an own-account or
household transfer (PRD §11.5) — and the previous approval flow is unchanged.

The document also enqueues one batch summary in the spirit of PRD §11.4
("`N transaksi ditemukan`", recorded, linked, and how many still need a decision)
instead of one chat message per row.

A nil bounded plane disables auto-confirm entirely, so intake still works when
the gateway is unavailable.

## Consequences

- Clear rows reach canonical state with no user interaction; only genuinely
  uncertain rows ask a question (PRD §11 exit criterion).
- A many-row screenshot produces one summary plus one question per uncertain row,
  not one card per row.
- Thresholds match the bank-email and Telegram category policies (MinTop .85 /
  MinMargin .20); no threshold was relaxed to reduce reviews.
- No provider-specific branch, no new table, no new dependency, and no extra
  bounded call per row.

