# ADR-052: Telegram runs without the judgment plane

## Status

Accepted — 2026-10-12. Amends ADR-038 and ADR-045 for the Telegram conversational agent.

## Context

ADR-038/045 made the System One (Jev) route decision the gate for every Telegram
mutation: a route of `OTHER_OR_UNCLEAR`, a rejected route, or no route removed
`record_transaction` from the agent. In production a plain follow-up ("jam 3 sore"
after the agent asked "pagi atau sore?") was rejected or classified unclear three
times in a row, so a clearly stated expense was never recorded. A guard meant to
prevent wrong writes was refusing correct ones, and the failure was invisible to
the user ("belum tercatat; coba lagi nanti").

## Decision

The worker no longer wires Jev into the Telegram processor. Go validation is the
only mutation authority: the agent receives the tools that apply to the household's
current state (pending action/batch, salary choice, review, merchant learning), every
write still passes deterministic validation, a complete extraction is accepted by
`directAcceptanceDecision`, and any dimension that cannot be settled
deterministically (for example an unknown category) goes to the Review Inbox rather
than being guessed. Exact server-bound replies keep their narrowed tool set.

Jev stays only as the evidence verifier for bank email, documents and financial
email; it never decides a Telegram turn.

## Consequences

- Telegram records what the user states; ambiguity becomes a review, not a refusal.
- Fast-path reads (spending, cashflow, savings, wealth) are served by the agent's
  native READ tools instead of a bounded route.
- `record_transfer` without an explicit purpose reports `JUDGMENT_UNAVAILABLE`
  until a deterministic purpose rule replaces it.
- The judgment code is deleted from `apps/worker/internal/telegram`: the Jev route
  fast path, route lanes, bounded workflow/transfer-purpose questions, turn
  telemetry, and the mutation-authority gate. The income-review confirmation
  prompt now reads its reply with fixed keywords (it previously needed Jev and
  failed without it). The shared `judgment` / `judgment/systemone` packages stay
  for the evidence verifiers.

## Amendment — undated entries record at message time (2026-10-12)

Removing Jev also removed the bounded `date_support` ruling that had been accepting
an undated in-the-moment entry ("beli bensin 26k") at the moment it was sent.
`directAcceptanceDecision` accepted only `USER_STATED` dates, so every such entry
became a "Tanggal transaksi belum ada" review (observed in production on
2026-10-12 after the ADR-052 deploy; the three previous undated entries of that
fortnight had been confirmed).

An entry that states no date is now accepted at the moment its message was sent
(`TimePrecision = OBSERVED_AT_PROCESSING`, transaction time equal to the processing
minute). A date the model supplied on its own — a past date the user never wrote —
is still never accepted and still becomes a review. The recorded time precision stays
in the proposal metadata for audit.
