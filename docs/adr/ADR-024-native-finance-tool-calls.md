# ADR-024: Native finance tool-call harness

## Status

Accepted — V3 P0 conversational agent.

## Decision

The LLM gateway may return native `function_call` items for an allow-listed
finance tool set. Go parses and validates the tool name and JSON arguments, then
executes the operation within household authorization and audit boundaries. The
gateway client never executes tools or receives database credentials.

## Consequences

- Natural conversation can be routed to query, create, and edit operations.
- Tool calls are auditable and independently testable from model wording.
- Unregistered or malformed calls fail closed.
