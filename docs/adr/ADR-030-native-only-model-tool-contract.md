# ADR-030: Native-only model tool contract

## Status

Accepted — 2026-09-01. **Partially superseded by ADR-033 for the Telegram conversational lane.**

## Decision

For strict single-result finance model workflows, Richmod requires exactly one
provider-native tool call. `tool_choice` is `required`; parallel calls are
disabled. Go strictly decodes tool arguments, validates domain rules, and owns
every state mutation.

JSON embedded in model prose, JSON-schema text output, and natural-language
fallback parsers are not production financial-action contracts.

ADR-033 supersedes the universal application of this rule for Telegram free-text
conversation. The conversational lane may return ordinary display-only text,
may call multiple validated READ tools, and may use multiple bounded model
phases. A conversational financial side effect still requires exactly one typed
side-effect tool call in that response and remains Go-owned.

## Consequences

- Strict extraction/classification and other single-result workflows may continue
  to use the original one-native-call contract.
- In the Telegram conversational lane, assistant prose is allowed but can never
  be parsed into a financial mutation.
- Unknown, unavailable, malformed, or unsafe conversational tool sets still fail
  closed under ADR-033.
- Gateway telemetry records call kind and selected tool names without financial
  content.
- Deterministic callbacks and server-rendered fallback replies remain valid
  without an LLM call.
- A valid allowed expense category may auto-confirm at category confidence
  `0.85` or above when overall extraction confidence is at least `0.90`; lower
  confidence or an unknown category still routes to review.
