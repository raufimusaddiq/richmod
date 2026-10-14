# F2 screenshot false merge — failing-before reproduction

Incident: two distinct same-merchant, same-amount e-wallet payments about
10 minutes apart (original screenshots show different printed transaction IDs,
RRNs and order IDs) were auto-linked into one canonical transaction.

## Proof

`TestScreenshotF2DisputedPairMustNotAutoLink`
(`apps/worker/internal/document/screenshot_f2_regression_test.go`) seeds a
confirmed transaction with its own screenshot evidence, then matches a second
screenshot with the same merchant and amount 10 minutes later, using synthetic
values. It asserts the second payment never reaches the strong auto-link
threshold (>= 0.90).

| Code | Result |
| --- | --- |
| Unmodified `main` `66c1a9c` (old-signature twin, run in a throwaway worktree) | **FAIL** — `score 0.95, 0.17h apart … would merge into the first` |
| Branch `fix/screenshot-false-merge` (legacy evidence, and distinct printed IDs) | **PASS** |

Old-code twin: identical facts, calling
`findScreenshotMatches(ctx, household, "EXPENSE", "7500", at, "Synthetic Store", true)`
(the pre-change signature) with a candidate seeded through the same
`transaction` / `transaction_proposal` / `transaction_evidence(TRANSACTION_SCREENSHOT)`
rows. The score is the unchanged `documentMatchScore`: 0.45 + 0.25 (≤1h) + 0.25
(same merchant) = 0.95, and the old `screenshotTimeConflict` only excluded
same-merchant gaps of one hour or more.

Run date 2026-10-14, PostgreSQL 17.4 disposable container, Go 1.27.0.
Limitation: the old twin could not exercise printed identifiers (the old
extractor had none); the branch test covers that case separately. The twin
itself is not committed because it cannot compile on the branch.
