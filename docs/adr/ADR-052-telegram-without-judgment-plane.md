# ADR-052: Telegram runs without the judgment plane

## Status

Accepted — 2026-10-12. Amends ADR-038 and ADR-045 for the Telegram conversational agent.
Amended 2026-11-05 (durable deferral of free text while the AI gateway is unavailable).

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

An entry that states no date is now accepted at the moment its message was sent. The
agent turn clock is the Telegram `message.date` (the worker clock only when the
update carries none), so a queue delay or retry across midnight cannot move an
entry onto another day. The observation is recorded as
`TimePrecision = OBSERVED_AT_PROCESSING` with the transaction time equal to that
message minute. A date the model supplied on its own — a past date the user never
wrote — is still never accepted and still becomes a review. The time precision stays
in the proposal metadata for audit.

## Amendment — durable deferral while the gateway is down (2026-11-05)

Without the judgment plane, every free-text Telegram turn needs the generative
gateway. When LiteRouter was unreachable a connection error was not recognized as
an outage: the job used all five attempts (about 30 s of backoff plus up to 160 s
per attempt) and then told the household to rephrase.

A first version of this amendment recorded "kopi 25rb"-style messages with a
deterministic parser. Review showed it cannot converge: "belum bayar kopi 25rb",
"rencana beli kopi 25rb", "harga kopi 25rb", "saldo 50rb", "beli kopi 25rb dong"
all have the shape of an expense, no word list separates a completed purchase
from a plan, a price or a request, and a guessed record — even one sent to
review — asserts that money was spent. It could also create a transaction in
place of an answer to a bound review card. That parser was removed.

Decision:

- Gateway failures are typed in the gateway package: a transport failure, a
  missing configuration or HTTP 408/429/5xx is *unavailable*; another 4xx is a
  *rejection* (never retried). A caller's own deadline stays a timeout.
- A free-text turn whose model call finds the gateway unavailable before
  anything was written creates nothing. The message is already durable as its
  source event (it stays `RECEIVED`). The household gets one reply per message:
  "AI Richmod sedang tidak bisa dihubungi. Pesanmu sudah kusimpan dan akan
  kuproses otomatis begitu tersambung lagi — belum ada yang dicatat." (a
  bookkeeping turn row, never replayed to the model, keeps it to once).
- The worker defers the job (`queue.Defer`): it goes back to PENDING with
  `run_after` 5 minutes ahead, doubling per deferral up to an hour, the attempt
  it used is given back, and `job.deferrals` counts the waits (migration 00092).
  A deferred message still keeps the household's order: when the gateway is back
  the older message runs first. A newer message that was never deferred is not
  held behind a deferred one, so a deterministic reply (for example `/help`) is
  still sent during the outage.
- When the gateway is back, the deferred message runs the full agent path
  exactly as if it had just arrived, so bound reviews, pending corrections and
  batches, and evidence are handled with their normal binding.
- A message still unanswered 24 hours after it arrived gives up: the job fails
  (visible in operations), the source event is marked FAILED, and the household
  gets "AI Richmod belum juga bisa dihubungi, jadi pesanmu belum bisa diproses dan
  belum ada yang dicatat. Silakan kirim ulang, ya." with a Tindakan item
  (`UNAVAILABLE`).

How "deterministic flows keep working when the AI gateway is unavailable"
(AGENTS.md) is met: every flow that needs no model keeps working during the
outage — button taps and callbacks (review cards, category choosers, pending
correction and batch confirmations, the card-choice buttons), proposal-keyed
review replies handled by the bound review lane (payslip pay date, missing
amount), `/help`, empty-message handling, and deterministic date parsing in those
lanes. Free text that needs the model to understand it waits instead of being
guessed.

Consequences: an outage delays an answer instead of inventing a record; the
household knows within seconds that the message is safe and nothing was
recorded. No model, infrastructure or authority is added.
