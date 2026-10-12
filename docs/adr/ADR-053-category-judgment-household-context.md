# ADR-053: Category judgments receive household context

## Status

Accepted — 2026-10-12. Amends ADR-038 for the bounded category questions of bank
email, receipts and transaction screenshots.

## Context

System One decides a category only from the state Richmod sends it. The bank-email,
receipt and screenshot category questions sent amount, merchant and description,
and described every category as "active expense category". The model could not tell
`Makanan & Minuman` from `Rumah Tangga` by anything but its slug, and had no way to
know that this household files `TINS MART` under groceries every time.

Read-only replay of 198 confirmed production expenses through `typesafe/jev-latest`
(thresholds unchanged: top ≥ 0.85, margin ≥ 0.20, confidence ≥ 0.60), scored against
the category the household actually holds. Parent-category choice, as bank email
asks it:

| Context | Correct top pick | Model abstains | Accepted by policy | Accepted and correct |
| --- | --- | --- | --- | --- |
| production (slug labels only) | 57% | 24% | 47% | 93 / 93 |
| + category names | 57% | 27% | 43% | 85 / 86 |
| + same-merchant history + local time | 78% | 14% | 63% | 122 / 125 |

Document questions offer every active category, parents and sub-categories together.
The same replay in that shape (35 categories, truth = the category actually held):

| Context | Correct top pick | Model abstains | Accepted by policy | Accepted and correct |
| --- | --- | --- | --- | --- |
| production (slug labels only) | 37% | 31% | 12% | 23 / 24 |
| names + same-merchant history + local time | 62% | 17% | 38% | 72 / 76 |

Names alone changed nothing; the household precedent is what moved the result. The
three accepted misses were ambiguous labels (a mart filed as groceries vs household
supplies, a haircut filed under health). Merchants with no history were unchanged.

## Decision

Every bounded category question for these three sources sends:

- each offered category with the household's own name (`Parent > Name` for a
  sub-category) instead of a neutral description;
- `household_history_same_merchant`: how many earlier **confirmed** expenses of the
  same merchant this household filed under each offered category (sub-categories roll
  up to an offered parent), counted only inside the household;
- `local_time` in `Asia/Jakarta` when the purchase moment is known.

The instruction text, probability policy, candidate set and Go resolution of the
chosen slug to a canonical ID are unchanged. History is evidence the model weighs,
not a rule: Go does not copy the precedent into the answer, and a model free to
disagree with it still can. A history lookup failure is a machine failure (retry),
never a silent downgrade to the thin context.

`BankEmailCategoryPolicyVersion`, `ReceiptCategoryPolicyVersion` and
`ScreenshotRowCategoryPolicyVersion` are bumped so stored `judgment_decision`
provenance distinguishes the new context.

## Non-goals

- Sub-category choice for bank email. At sub-category level the same replay accepted
  fewer rows than today (42% vs 47%) with 6 wrong in 69; it is not shipped until a
  measured design proves otherwise.
- Changing evidence verification, thresholds or the Review Inbox contract.

## Consequences

- Expected effect, to be re-measured from `judgment_decision` outcomes and the
  `AMBIGUOUS_CATEGORY` review rate after deployment: fewer category reviews at
  unchanged policy strictness.
- The replay covers one household and 198 rows, and part of its truth may have been
  assigned by earlier merchant memory; the result is a direction, not a guarantee.
- Documents offer every active category (parents and sub-categories) while bank email
  offers parents only; the replay measured the bank-email shape.
