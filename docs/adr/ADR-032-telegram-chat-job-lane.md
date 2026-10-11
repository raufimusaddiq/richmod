# ADR-032: Dedicated Telegram CHAT lane

## Status

Accepted — 2026-09-01. Superseded in part — 2026-10-11: the CHAT lane is
retired. Migration 00048 had already stopped routing jobs to it, so free-text
Telegram jobs ran in `DEFAULT`; migration 00081 keeps them there on purpose.
One `DEFAULT` consumer keeps each person's messages in order (the CHAT claim
had no per-chat ordering), and `INTERACTIVE` still isolates callbacks from LLM
work. `WORKER_CHAT_CONCURRENCY` is removed; the `CHAT` value stays permitted by
the `job.lane` check constraint but no job uses it.

## Decision

PostgreSQL jobs use four lanes: `INTERACTIVE`, `CHAT`, `DEFAULT`, and
`BACKGROUND`. Free-text Telegram jobs use `CHAT`; callbacks and outbound Telegram
transport stay in `INTERACTIVE`.

## Consequences

- LLM chat work cannot occupy callback ACK/action capacity.
- `WORKER_CHAT_CONCURRENCY` configures 1–4 independent CHAT consumers.
- Queue/admin reporting exposes CHAT separately.
