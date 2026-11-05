# ADR-052: Telegram runs without the judgment plane

## Status

Accepted — 2026-10-12. Amends ADR-038 and ADR-045 for the Telegram conversational agent.
Amended 2026-11-05 (deterministic expense fallback while the AI gateway is unavailable).

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

## Amendment — deterministic expense fallback while the gateway is down (2026-11-05)

Without the judgment plane, every Telegram turn needs the generative gateway. When
LiteRouter was unreachable a connection error was not recognized as an outage: the
job used all five attempts (about 30 s of backoff plus up to 160 s per attempt)
and then told the household to rephrase. AGENTS.md requires deterministic flows to
keep working when the gateway is unavailable, and recording a plain expense is a
deterministic flow.

Decision:

- Gateway failures are typed in the gateway package: a transport failure, a
  missing configuration or HTTP 408/429/5xx is *unavailable*; another 4xx is a
  *rejection*. A caller's own deadline stays a timeout.
- On the first model call that fails as unavailable (or with no conversational
  gateway wired), before any side effect, Go tries `parseSimpleExpense` on the
  message. It accepts exactly one amount (`25rb`, `25 ribu`, `5k`, `50.000`,
  `Rp 25.000`, `1,5jt`, a bare number of at least 500) and one to four plain
  words, after dropping a leading `beli`/`bayar`/`buat`/`untuk`. It refuses
  anything else: a question, a second amount, a date or time word, income,
  transfer, savings, debt or correction words, negation or intent ("belum bayar",
  "rencana beli", "mau", "nanti", "kalau"), a bill, budget or estimate, several
  items, punctuation such as `?`/`:`/`+`. A category-only review cannot establish
  that an expense happened, so only a plain statement of one is recorded. It
  never guesses.
- A parsed message is recorded only when it states the purchase (a leading
  `beli`/`bayar`/`jajan`/`isi`) or its words name something the household already
  spent on (a confirmed expense's description or counterparty, a household
  merchant, a category name): "kopi 25rb" after earlier coffee records, "harga
  kopi 25rb" or "saldo 50rb" never does. It never runs for a message an open
  workflow could own: an exact Reply, a bound review card or merchant question, a
  pending correction, batch or salary choice, or bound/recent evidence.
- A parsed message is recorded as an EXPENSE in NEEDS_REVIEW at the message time,
  with no category and no merchant (the words become the description), with
  `decision_source=DETERMINISTIC_FALLBACK` in the proposal metadata, evidence and
  audit row, and the normal review card (AMBIGUOUS_CATEGORY) in the chat. It is
  never confirmed from the fallback: the household picks the category on the
  card. The reply says the AI is unreachable, what was recorded, and that it is
  not final. Claiming the source event in the same transaction keeps it
  idempotent.
- A message that does not parse fails as *unavailable*: one quick retry, then the
  terminal notice "Asisten AI Richmod sedang tidak bisa dihubungi … belum ada yang
  dicatat", which also says simple expenses can still be recorded. A gateway 4xx is
  never retried.

Consequences: an outage costs the household the category tap, not the record;
questions and complex entries get an honest notice within seconds instead of
minutes. The fallback adds no model, no infrastructure and no authority: Go
writes exactly what a NEEDS_REVIEW record_transaction would, minus the model's
category.
