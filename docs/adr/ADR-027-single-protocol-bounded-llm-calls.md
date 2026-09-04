# ADR-027: Single-protocol bounded LLM calls and redacted observability

## Status

Accepted — correctness/performance remediation Release 2.

## Decision

`LLM_GATEWAY_PROTOCOL` selects exactly one Cloud LLM Gateway protocol:
`responses` (the default and production setting) or `chat_completions`. A call
never retries through a different protocol after HTTP, transport, decoding, or
schema failure. Responses are capped at 2 MiB and must contain exactly one JSON
value with no trailing content.

Go owns task budgets: Telegram 10 seconds, bank extraction 45 seconds total,
document stages 60 seconds, and insights 30 seconds. Bank extraction permits one
corrective call only after a successful but schema-invalid response. Transport
failure is left to the bounded PostgreSQL job retry policy.

The additive `llm_call` table is restricted to task/protocol/model/status,
duration, usage, cost, attempt, and household metadata. Prompt, response, email,
document, and message content are prohibited.

Responses calls use `stream:true` and parse server-sent events into the same
bounded adapter contract. When a configured protocol returns exactly one allowed
native function call, provider reasoning or other auxiliary text is ignored. It
is never parsed as financial data. Unknown, missing, multiple, or JSON-invalid
function calls still fail closed and remain subject to Go DTO validation.

## Consequences

- One logical attempt has a predictable outbound request count.
- Malformed provider responses fail closed and cannot trigger a hidden second call.
- Financial workflows remain independent of best-effort metric persistence.
