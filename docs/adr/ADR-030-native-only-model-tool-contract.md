# ADR-030: Native-only model tool contract

## Status

Accepted — 2026-09-01.

## Decision

Every Richmod finance model invocation requires exactly one provider-native tool
call. `tool_choice` is `required`; parallel calls are disabled. Go strictly
decodes tool arguments, validates domain rules, and owns every state mutation.

JSON embedded in model prose, JSON-schema text output, tool continuations, and
natural-language fallback parsers are not production finance contracts.

## Consequences

- Provider auxiliary prose is ignored.
- Unknown, zero, multiple, malformed, or trailing tool arguments fail closed.
- Gateway telemetry records native call kind and selected tool name.
- Deterministic callbacks and server-rendered replies remain valid without an
  LLM call.
- A valid allowed expense category may auto-confirm at category confidence
  `0.85` or above when overall extraction confidence is at least `0.90`; lower
  confidence or an unknown category still routes to review.
