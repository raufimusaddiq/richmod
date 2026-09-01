# ADR-032: Dedicated Telegram CHAT lane

## Status

Accepted — 2026-09-01.

## Decision

PostgreSQL jobs use four lanes: `INTERACTIVE`, `CHAT`, `DEFAULT`, and
`BACKGROUND`. Free-text Telegram jobs use `CHAT`; callbacks and outbound Telegram
transport stay in `INTERACTIVE`.

## Consequences

- LLM chat work cannot occupy callback ACK/action capacity.
- `WORKER_CHAT_CONCURRENCY` configures 1–4 independent CHAT consumers.
- Queue/admin reporting exposes CHAT separately.
