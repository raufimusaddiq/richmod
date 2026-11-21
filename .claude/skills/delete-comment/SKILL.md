---
name: delete-comment
description: Purge source-code comments from the Richmod repo and ratchet the ban. Use when `node scripts/check-comments.mjs` fails, when legacy comments remain in the baseline, or when asked to delete, audit or classify code comments. Classifies each comment, moves real decisions to the ADR log, files issues for hacks, deletes the rest.
---

# delete-comment

Comments are banned in this repo (`scripts/check-comments.mjs`, enforced in CI). Code says what; tests say what must hold; `docs/adr/README.md` says why. A comment is one of four things, and each has a fixed action:

| Category | Meaning | Action |
| --- | --- | --- |
| **T** Tautology | Repeats what the code, name, signature or test name already shows. Default when no non-obvious *why* is present. | Delete |
| **S** Stale | Refers to code, behaviour, files or ADRs that no longer exist or hold. Verify by grep before claiming. | Delete |
| **E** Excuse | Exists to justify an awkward implementation: workaround, "legacy", "for now", magic number, swallowed error, duplicated logic, prose narrating convoluted code. Test: if the code were clean, would the comment still be needed? No means Excuse. | Delete and file an issue to fix the implementation |
| **D** Decision Record | A non-obvious decision a competent engineer cannot recover from code, tests and the ADR log, whose violation would cause a bug, security or financial-correctness error. Test: still needed with clean code, and states a why. | Move into an ADR, then delete |
| **K** Keep | A functional directive, not prose (`//go:build`, `// +goose`, `eslint-disable`, `// Output:`). | Add to `isDirective` in the scanner so it stops counting |

Mixed block: priority E > D > S > T.

## Tools

- `node scripts/check-comments.mjs` runs the ratchet. `--update` rewrites `scripts/comment-baseline.json` (per-file comment counts that may only go down). `--extract` prints every comment as JSON.
- `node scripts/delete-comments.mjs [paths...]` deletes every comment in the given files (default: every file that has any). It tidies blank lines, removes JSX `{/* */}` containers, runs gofmt on Go files that were gofmt-clean, and refuses to write a file unless the non-comment, non-whitespace content is unchanged.
- `node scripts/comment-audit.mjs <chunk|merge|adr-in|adr-merge|issues> <dir>` drives the audit below. `<dir>` is a scratch directory outside the repo.
- Scanner correctness was checked against `go/scanner` (Go) and Babel's parser (JS); extend `scripts/check-comments.test.mjs` when a new construct is misread.

## Procedure

Work in your own branch and worktree (see `AGENTS.md`).

1. **Inventory.** `node scripts/check-comments.mjs` shows what remains. Record the base commit: `git rev-parse HEAD`.
2. **Chunk.** `node scripts/comment-audit.mjs chunk <dir> 13` writes `chunks/chunk-NN.txt` (comment blocks with a few lines of following code) and `chunk-NN.ids.json`. Scale the chunk count so each chunk is about 110k characters.
3. **Classify, in parallel.** Spawn one read-only subagent per chunk, all in one message. Brief each one with the category table above, the instruction to read its chunk file and open the real source (Read/Grep) when the snippet is not enough, and this output contract: write `<dir>/cls/chunk-NN.json`, one JSON array with every id exactly once:
   - `{"id":1,"c":"T"}`
   - `{"id":2,"c":"S","why":"..."}`
   - `{"id":3,"c":"E","title":"<=80 chars, imperative","hack":"...","fix":"..."}`
   - `{"id":4,"c":"D","topic":"2-5 words","decision":"1-2 self-contained sentences with the reason","adr":["ADR-037"],"existing_adr":"ADR-055"}` (`existing_adr` only if the log already states it; grep `docs/adr/README.md` first)
   - `{"id":5,"c":"K","why":"..."}`
   Subagents must not edit the repo. Then `node scripts/comment-audit.mjs merge <dir>`; it fails if any id is missing, duplicated or malformed.
4. **Decision records to ADRs.** `node scripts/comment-audit.mjs adr-in <dir>` clusters the D entries (without `existing_adr`) into `adrin/cluster-NN.txt`. Spawn one subagent per cluster as a stricter second reviewer: keep an item only if it is a non-obvious why whose violation causes a real bug, merge duplicates, drop what is behaviour narration or already in the log. It writes `adrout/cluster-NN.md` (sections headed `## ADR-NEW: <title>`, then `*Accepted · YYYY-MM-DD*`, a scope sentence, and bullets `- **name**: decision and reason. (`symbol`)`; functions and tables by name, never line numbers) and `adrout/cluster-NN.json` mapping every input id to `kept`, `merged:<id>` or `dropped:<reason>`. Read a few results, then `node scripts/comment-audit.mjs adr-merge <dir>`, which numbers the entries after the highest existing ADR, adds the index rows and updates the "Adding one" line.
5. **Excuses to issues.** `node scripts/comment-audit.mjs issues <dir> <base-sha>` drafts one issue per distinct title with permalinks to the original comment. Merge drafts that are the same cleanup (for example every "remove legacy fallback"), then `gh issue create --label enhancement --label status:backlog`. Never file an issue for something that is not a real implementation problem.
6. **Delete.** `node scripts/delete-comments.mjs`, then `node scripts/check-comments.mjs --update`.
7. **Verify.** `go build ./... && go vet ./...` and `go test ./...` in `apps/api`, `apps/reviewdomain`, `apps/worker`; `npm run build && npm test` in `apps/web`; `node --test scripts/*.test.mjs`. Tests that assert on source text sometimes match a comment; fix the test to assert on code, never restore the comment.
8. **Ship.** Follow the PR flow in `AGENTS.md` (branch, commit, push, PR, wait for CI and Hermes Review).

## Leaving some comments (ratchet)

If a purge cannot finish, run `node scripts/check-comments.mjs --update` so the remaining counts become the baseline. CI then fails if any file gains a comment, and also fails if a file drops below its baseline until `--update` records the progress. The goal is an empty `{}` baseline.

## Retrieving a deleted comment

The original text stays in git history: `git log -S'phrase' -- path`, or `git show <base-sha>:path`.
