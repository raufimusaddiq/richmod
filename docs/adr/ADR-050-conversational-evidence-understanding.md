# ADR-050 — Conversational Evidence Understanding (CEU)

## Status

Accepted — 2026-10-03 (design merged in PR #283; owner decisions recorded below).

## Context

The household can already send evidence (receipts, payslips, screenshots,
transfer proofs) and chat about finances. The two paths barely meet:

- An uploaded image has no footprint in the conversation. The next typed message
  ("yang ini masuk makan ya") cannot be tied to it unless a review card exists.
- Replies bind only to review cards. The bot's other messages are not bindable,
  and a reply that resolves to nothing has nothing to fall back on.
- `telegram_turn_reference` can hold only transaction references.
- Evidence-derived text (caption, extracted merchant) has no untrusted-data
  boundary for the model.

[`CEU-00-architecture-audit.md`](../audits/CEU-00-architecture-audit.md) records
the code-level findings. BDR-005 closed UIR and SAVR before this work.

## Decision

CEU adds **deterministic conversational binding and a model-safe evidence
context**. It adds no decision authority.

1. **Binding is server-owned and ordered.** Exact reply, then active
   evidence-backed review, then a valid opaque ref, then the one immediately
   preceding evidence, then a bounded recent set, then clarification. A generative
   model never chooses evidence when a deterministic level can. An exact reply
   that resolves to nothing is terminal; it does not fall back.
2. **Three reference classes stay distinct** — turn-local, cross-turn bounded,
   canonical. Canonical ids (`source_event.id`, `document.id`, `transaction.id`,
   `review_item.id`, evidence row ids) are never model-visible and never accepted
   as arguments. Cross-turn evidence refs are opaque, household + Telegram user +
   chat scoped, expiring, and resolved only by Go. Replay of an expired ref is
   never authority.
3. **The evidence unit is the `document`**, which covers albums. Transactions are
   linked through the existing `transaction_evidence` rows; CEU does not change
   how evidence attaches.
4. **The Evidence Context Package** separates `observed` (extractor output,
   unverified), `canonical` (Go-stored, only after finalization) and `workflow`
   (server-owned missing dimensions and allowed actions). It contains no UUIDs,
   OCR text, storage refs, or confidence values, and wraps all evidence-derived
   strings in `<untrusted_evidence_text>`.
5. **No new decision path.** CEU introduces no SIDE-EFFECT tool and no
   finalizer. Corrections flow through the existing `resolve_review`,
   `propose_transaction_correction`, SAVR, ReviewDomain and Go canonicalization.
   The model interprets only the residual the server says is missing; accepted
   dimensions are not re-decided.
6. **Storage is minimal.** One additive migration widens
   `telegram_turn_reference` to hold evidence refs. Recording the bot's outbound
   message id for evidence notices, and an indexed chat id on `source_event`, are
   needed by CEU-02 per the owner decisions below. No evidence-memory table.
7. **Failure is not human work.** A CEU interpretation or provider failure opens
   no review item; a material household decision does.
8. **Telemetry is bounded**: binding-level and resolver-outcome counters only.

## Owner decisions (2026-10-03)

1. **Evidence notices: yes.** Auto-resolved receipts and payslips send a short "recorded" notice to the chat the evidence came from, and the notice is bindable so a reply to it resolves the evidence (CEU-02). This adds the outbound-message binding.
2. **Binding window and ambiguity.** "Immediately preceding" evidence is the newest from this user in this chat within 10 minutes. Ambiguity defaults to one clarification question, not a bounded Jev choice (CEU-03).
3. **Indexed chat id: yes.** `source_event` gains a nullable indexed `telegram_chat_id` so a reply to an upload binds by household + chat + message id (CEU-02). Because notices must reach the originating chat, this also replaces the first-active-identity destination the document pipeline uses today.

## Consequences

- A follow-up after an upload binds without the user restating context, and
  ambiguity yields one question instead of a guess.
- Conversation memory remains deterministic and compact; compaction cannot
  resurrect an expired ref.
- Exact-binding surfaces are: reply to the user's upload, reply to a bound
  evidence notice, and reply to a review card.
- Telegram image/document evidence only. Email evidence reaches a conversation
  only through a review or transaction link.

Corpus, metrics, known limits and the freeze condition are recorded in
[CEU-07](../audits/CEU-07-corpus-and-metrics.md).

## Rejected alternatives

- **A CEU agent or decision system.** Duplicates SAVR/ReviewDomain authority.
- **Letting the model pick among recent evidence.** Violates "Go owns identity".
- **A new evidence-memory table or vector/RAG store.** The data already lives in
  `document`, `document_extraction`, `transaction_evidence`; a second copy can
  drift.
- **Exposing extractor confidence.** It would become a hidden semantic threshold.
- **A new correction tool.** The existing bound-workflow tools already express it.
- **Replaying evidence refs from memory.** The analytics `category_ref` incident
  class; refs are re-derived from the database each turn.

## Related

ADR-004, ADR-009, ADR-020, ADR-024, ADR-030, ADR-031, ADR-033, ADR-037, ADR-038,
ADR-039, ADR-046, ADR-047, ADR-048; BDR-005.
