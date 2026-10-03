# CEU Execution Plan

**Status:** CEU-01 through CEU-07 implemented; **corpus-complete, production observation open** (see [CEU-07](../audits/CEU-07-corpus-and-metrics.md)). Not frozen until the owner accepts the observation.
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

## Delivery

| Slice | PR | Notes |
| --- | --- | --- |
| CEU-00 | #283 | audit, ADR-050, this plan |
| CEU-01 | #284 | evidence refs, context package, untrusted boundary |
| CEU-02 | #285 | reply binding, bindable notices, upload chat id |
| CEU-03 | next | recent evidence, `get_evidence_context`, per-document ref keys |
| CEU-04 | next | recent-evidence review binding, no duplicate harvest, correction guidance |
| CEU-05 | next | duplicate candidates, `MERGE_EXISTING` through the canonical merge |
| CEU-06 | next | a reply to an upload or notice continues its open review |
| CEU-07 | next | corpus, Operations counters, correction e2e, freeze record |

One deviation from the original slice text: `search_transactions` is **not** extended with evidence summaries (CEU-03 plan item). Evidence reaches a transaction conversation through `bound_evidence` and `get_evidence_context`, which was enough; extending a second tool was YAGNI.

## CEU-00 — audit and design

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

- Interpret residuals ("125 ribu", "kemarin", "merchantnya Mirota") against the
  bound evidence and hand them to the existing evidence-backed review /
  `resolve_review` / `propose_transaction_correction` paths.
- No new finalizer; known dimensions are not re-decided.

Tests: known amount + date only, amount correction without a duplicate
transaction, SAVR corpus unchanged.

## CEU-05 — evidence ↔ existing transaction

- Use the existing candidate generator and candidate-choice path for "struk ini buat
  transaksi yang tadi". Candidates are anonymous; the model never sees ids.
- A follow-up correction on evidence already linked through `transaction_evidence`
  targets that transaction.

Tests: link without duplication, category correction on a linked receipt,
duplicate delivery.

## CEU-06 — natural review continuation

- Evidence-backed review replies like "tanggal 25" resolve against the stored
  ReviewDecision `missing` set only.

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
