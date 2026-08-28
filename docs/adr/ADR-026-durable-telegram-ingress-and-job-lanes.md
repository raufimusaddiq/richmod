# ADR-026: Durable Telegram ingress and isolated job lanes

## Status

Accepted — availability remediation Release 1.

## Decision

The Telegram webhook authenticates and validates a bounded `review:` callback
grammar, then atomically persists source evidence and a queue job before returning
HTTP 204. The API has no Telegram bot token and makes no Telegram network call.

The worker acknowledges callbacks first with a 1.5-second budget. ACK failure is
logged but does not block the deterministically bound review action. Callback,
send, and edit work is `INTERACTIVE`; ordinary Telegram text is `DEFAULT`; bank,
document, image, payslip, screenshot, and insight work is `BACKGROUND`.

PostgreSQL assigns lanes with a trigger and unfinished jobs are backfilled. The
worker runs one independent consumer per lane, yields after at most 25 jobs on
both success and failure, and therefore runs at most two non-interactive LLM jobs
concurrently. Job attempt start and terminal finish timestamps support queue-wait
and execution-duration reporting.

## Consequences

- Slow LLM and document work cannot occupy the callback consumer.
- API availability no longer depends on Telegram API latency or credentials.
- Producers cannot accidentally bypass queue isolation by omitting a lane.
- Callback acknowledgement remains transport feedback, not proof of mutation.
