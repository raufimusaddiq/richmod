# ADR-027: Single-protocol bounded LLM calls and redacted observability

## Status

Accepted — correctness/performance remediation Release 2. **Amended by ADR-033 for Telegram free-text conversation.**

## Decision

`LLM_GATEWAY_PROTOCOL` selects exactly one Cloud LLM Gateway protocol:
`responses` (the default and production setting) or `chat_completions`. A call
never retries through a different protocol after HTTP, transport, decoding, or
schema failure. Responses are capped at 2 MiB.

Go owns task budgets. Bank extraction remains 45 seconds total, document stages
60 seconds, and insights 30 seconds. Telegram callbacks retain their deterministic
interactive path. Under ADR-033, Telegram free-text conversation has a 20-second
overall turn budget, an 8-second per-model-call deadline, at most 5 model phases,
at most 4 READ calls in one model response, at most 8 READ calls in one turn, and
at most one side effect in one turn.

Bank extraction permits one corrective call only after a successful but
schema-invalid response. Transport failure is left to the bounded PostgreSQL job
retry policy.

The additive `llm_call` table is restricted to task/protocol/model/status,
duration, usage, cost, attempt, call kind, tool name(s), and household metadata.
Prompt, response, email, document, message, tool-argument, and financial-result
content are prohibited from operational telemetry.

Strict single-result model workflows continue to validate the one allowed native
function call and ignore provider auxiliary prose. The Telegram conversational
adapter instead accepts the response shapes defined by ADR-033: display-only
text, an allow-listed READ batch, or exactly one side-effect call. It still fails
closed on unknown/malformed calls and never parses prose as financial action.

## Consequences

- One deployment uses one configured provider protocol; there is no hidden
  protocol fallback.
- Conversational turns have predictable server-owned phase/read/write limits
  even though they may contain several model calls.
- Malformed or unsafe provider responses fail closed.
- Financial workflows remain independent of best-effort metric persistence.
