# CEU Execution Plan

**Status:** accepted (ADR-050); CEU-01 in progress
**Baseline:** `main@be6be45e1b813bbee80de67577386c93193c2035`
**Audit:** [`audits/CEU-00-architecture-audit.md`](../audits/CEU-00-architecture-audit.md)
**ADR:** [`adr/ADR-050-conversational-evidence-understanding.md`](../adr/ADR-050-conversational-evidence-understanding.md)

## Operating rule

One coherent capability per PR. Follow `docs/runbooks/sprint-delivery.md`:
fetch exact latest `main`, linked worktree, re-audit the named call sites, narrow
tests, docs in the same branch, wait for CI and exact-head Hermes Review, bundle
review fixes into one push, merge with a merge commit, track `main` before the
next slice. Never queue dependent PR heads.

Each slice that changes the schema updates `docs/DATABASE_SCHEMA.md` in the
same branch and is applied to disposable PostgreSQL from zero.

---

## CEU-00 — audit and design (this PR)

Docs only: audit, ADR-050 (Proposed → Accepted on merge), this plan. No runtime
code.

## CEU-01 — bounded evidence references

- Additive migration widening `telegram_turn_reference` (`EVIDENCE`, `…_ev<n>`).
- Distinct Go types for turn-local ref, cross-turn evidence ref, canonical id.
- Server issuance (idempotent, `ON CONFLICT`), deterministic resolver
  (household + user + chat + expiry + row state), stale/invalid replies.
- Shared `<untrusted_…>` helper that neutralizes embedded closing tags; apply to
  user, ledger and evidence wrappers; one prompt sentence.
- Evidence Context Package builder (fixed fields, ≤ 5 items, no UUIDs).
- Telemetry counters for resolver outcomes.
- No semantic behavior: nothing binds yet.

Tests: lifetime-class separation, canonical-id leak walk, expired / cross-household
/ cross-chat refs, idempotent issue, prompt-injection fixtures.

## CEU-02 — reply-to-evidence binding

- Resolve `reply_to_message_id` against the user's upload
  (`source_event.telegram_message_id` + chat; album → its `document`) and existing
  review cards. Terminal when it resolves to nothing.
- Add the evidence summary to `active_review` for evidence-backed reviews.
- Owner decided yes to both: add the outbound-message binding table and the
  indexed `source_event.telegram_chat_id` (nullable, backfilled from the stored
  payload).
- Send a short "recorded" notice for auto-resolved receipts/payslips to the chat the
  evidence came from (replacing the first-active-identity destination), recorded in
  the binding table so a reply to it binds.

Tests: receipt A/B reply-to-A, unresolved reply does not fall back, album reply.

## CEU-03 — recent evidence context

- Binding levels 4–6 with named, single-location constants.
- Turn context carries `bound_evidence` / `recent_evidence`; one READ tool
  `get_evidence_context(evidence_ref)`; `search_transactions` shows linked evidence.
- One clarification on ambiguity (owner decision: no bounded Jev choice in v1;
  the 10-minute window is the default).
- Optional USER-side evidence turn for conversation history.

Tests: unique bound, ambiguous → clarification, no recency spill across users/chats,
provider failure creates no review.

## CEU-04 — conversational evidence correction

**As delivered.** No new finalizer and no new tool.

- Prompt rules for evidence corrections: category/description/date go through
  `propose_transaction_correction` with `canonical.transaction_ref`; facts for a receipt
  with an open workflow go through the review tool for the missing facts only; shown
  facts are never re-asked or overwritten; the amount or merchant of an already
  recorded transaction cannot be changed from chat and the model must say so.
- **No duplicate ledger row, enforced by Go.** With evidence in the immediate window,
  a bare amount is not harvested as a standalone transaction (`HasRecentEvidence`,
  immediate window only, so an ordinary new expense keeps its fast path), and
  `record_transaction` refuses (`ALREADY_RECORDED_FROM_EVIDENCE`) a proposal with the
  same type and amount as a transaction that fresh evidence already produced, when
  the merchant is absent or the same. A different merchant at the same price is
  allowed; an old receipt is left to the existing same-merchant edit guard.
- **Deliberately not done:** binding a recent document's open review without a reply.
  The route that would use such a binding (`REVIEW_INTERACTION`) is answered
  terminally by the fast path, so the binding would never reach the model; it was
  built, found unreachable in review, and removed. Reviews are resolved by an exact
  reply (CEU-02/06) or by the existing deterministic reply lane.

Tests: known amount + date only, amount correction without a duplicate
transaction, SAVR corpus unchanged.

## CEU-05 — evidence ↔ existing transaction

**As delivered.** No new finalizer.

- A possible-duplicate document's evidence context lists its candidates (the existing
  `duplicateCandidateRows` query, now shared with the Telegram callback flow) as
  opaque expiring refs with untrusted-wrapped labels; no ids reach the model.
- `resolve_review` gains `MERGE_EXISTING` for a `POSSIBLE_DUPLICATE` review only. Go
  resolves the ref for this household, user and chat, requires the target to be one of
  the review's current server-computed candidates, then runs the single canonical merge
  (`reviewdomain.MergeDuplicateReview`). Stale, foreign, made-up or non-candidate refs
  change nothing.
- Merged and refused outcomes have distinct deterministic messages, so they stay
  distinguishable when the model call fails.
- A follow-up correction on evidence already linked through `transaction_evidence`
  targets that transaction (CEU-04 tool scope; staged, not applied).

Tests: link without duplication, category correction on a linked receipt,
duplicate delivery.

## CEU-06 — natural review continuation

**As delivered.** No new lane and no new tool; an existing deterministic lane is
reached from the production entry.

- **Finding.** Typed text is queued as `PROCESS_TELEGRAM_TEXT` and handled by
  `ProcessAgent`; `Process` is entered only for callbacks. The conversational agent has
  no binding for **proposal-keyed** reviews (payslip pay date, missing amount, document
  reviews), and the deterministic lane that answers them (`processBoundReview`) was
  reachable from callbacks only. A typed date reply to such a card therefore reached the
  model and left the review open, whether it replied to the card or to the upload.
- **Fix, at `ProcessAgent`.** An **exact** reply to a proposal-keyed review is answered
  by that deterministic lane, with its own state checks, when it replies to the card, to
  the document's upload, or to a bound evidence notice
  (`replyTargetForEvidenceReview`, `repliesToProposalReview`). The target comes only
  from the exact message replied to, scoped to household + chat; it never searches for
  "the latest" review. A lookup failure returns an error and the job retries instead of
  silently rerouting the reply.
- **Scope kept narrow.** Only explicit replies, and only proposal-keyed reviews.
  Transaction-keyed reviews stay with the agent (tested), and a reply-less message is
  never taken by this lane: chat state alone does not own a turn.
- **Known difference.** An expired review is not continued from an upload or notice,
  and a typed reply to an expired card is not revived either: the agent entry has no
  projection renewal (`renewExpiredReviewProjection` runs only in `Process`, i.e. for
  callbacks). Reviving an expired review remains a button/callback action.
- Known facts are not re-decided: the lane supplies only the missing fact, and the tests
  assert the amount is untouched, exactly one transaction exists, and the model was
  never called.

Tests: missing `transaction_at` does not re-ask amount/category.

## CEU-07 — corpus, production observation, freeze

- Corpus covering the audit §9 matrix; combined drift guard.
- Owner-household observation using normal use, no seeded production data;
  rare families may stay `PRODUCTION_UNOBSERVED` with corpus proof (BDR-005 model).
- Tune the recency constants from binding-level counters, then freeze CEU
  except for defects against its approved contract.

---

## Out of scope (YAGNI)

See audit §12. Anything not listed in a slice needs an ADR first.
