# ADR-049 — Failed Sources Surface as Dismissable Actions

## Status

Accepted — 2026-10-02.

## Context

`/analytics` counts source events that are still `RECEIVED`, `PROCESSING`, or
`FAILED` as "sumber belum selesai diproses" and links to the Inbox. The Inbox
lists review items and integration actions only, so the household saw a count
with nothing behind it (8 items on analytics, an empty Inbox).

The events were real failures with no owner:

- bank emails whose extraction was unusable (`INVALID`) or whose provider call
  failed (`TRANSPORT_FAILED`), with no transaction recorded;
- button taps whose job died on an error and were never finalized;
- a typed message whose model call timed out and was never answered.

ADR-048 is explicit that a machine failure must not open a review, because there
is no household fact to ask for. That rule stays. What was missing is somewhere to
say "this did not work" that the household can see and close.

## Decision

A source event that reaches a terminal failure leaves one dismissable item in the
Inbox's **Tindakan** tab, an `integration_action` of type `SOURCE_PROCESSING` /
`SOURCE_FAILED`:

- **When.** A final failure is recorded in the same transaction that marks the job
  `FAILED` (`queue.FailWithHook`), so the job is never failed without the item. A
  bank email whose extraction is `INVALID` is final immediately (the job
  succeeds), so it is recorded when that outcome is persisted. A retryable failure
  is recorded only when the job gives up, so a retry that succeeds leaves no
  stale item.
- **What.** Plain copy per source (`reviewdomain.FailedSourceCopy`): what
  happened, whether any transaction was recorded, and what to do. It is an
  action, not a review: there is nothing to decide, only something to know and
  close.
- **Idempotent.** The dedupe key is the source event ID, so a replay or a second
  worker cannot create a second item.
- **Closing it finalizes the event.** Resolving the action sets the source event to
  `IGNORED` in the same transaction, scoped to the household and only while the
  event is still unfinished (`RECEIVED`, `PROCESSING`, `FAILED`); an event that
  was answered in the meantime keeps its state. The change is audited
  (`IGNORE_FAILED_SOURCE`). Analytics counts only unfinished events, so it stops
  counting the source.
- **Telegram taps and messages also get a plain reply** when the job dies (a typed
  message: "Richmod lagi lambat menjawab…"; a tap: "Tombol itu belum bisa
  diproses…" plus the dead buttons retired), claimed in the same transaction so
  it is sent once and never contradicts a message that was answered.
- **Analytics links to the Tindakan tab** (`/inbox?view=actions`) with the label
  "Buka Tindakan".

No schema change: `integration_action` already has free-text type columns, a
per-household dedupe key, and a JSON metadata column that carries the source event
ID.

## Consequences

- The count on `/analytics` and the Inbox now agree: every unfinished source that
  reached a terminal failure has a visible, closable item, and the Inbox badge
  includes them.
- Closing the items is owner-only, like every integration action.
- Failures from before this change have no item. They stay counted for the cycle
  they fall in until they are finalized by hand.
- A source stuck in `RECEIVED` or `PROCESSING` with no failed job (for example, a
  job lost outright) is still counted but has no item. That is a different fault
  and is left visible on purpose.
