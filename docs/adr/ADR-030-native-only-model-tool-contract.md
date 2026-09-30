# ADR-030: Native-only model tool contract

## Status

Accepted — 2026-09-01. Amended — 2026-09-30.

ADR-033 still governs bounded multi-phase conversational orchestration, but its
raw final-text response shape is superseded by this amendment: production LLM
responses use provider-native tools as the machine boundary, including a native
rendering tool for user-facing prose.

## Decision

Provider-native tools are the machine contract for every production LLM
interaction.

For strict single-result finance workflows, Richmod requires exactly one native
tool call. `tool_choice` is `required`; parallel calls are disabled. Go
strictly decodes tool arguments, validates domain rules, and owns every state
mutation.

Conversational lanes may use multiple bounded model phases and validated READ
tool calls as defined by ADR-033. However, a conversational model does not finish
with raw assistant text. When it wants to speak to the user it calls a
server-owned rendering tool such as `respond_to_user`. The rendering tool may
carry a free-form `message` string (and bounded supporting references where
useful). Go validates the tool envelope and forwards the message; it does not
parse the prose back into semantic or financial state.

This is intentionally different from asking the model to "return JSON".
Structured data belongs in native tool arguments/results. Natural language
belongs inside the rendering tool's message.

JSON embedded in model prose, JSON-schema text output, ad-hoc structured text,
and natural-language fallback parsers are not production contracts.

A conversational financial side effect still requires exactly one typed
side-effect tool call in that response and remains Go-owned.

## Consequences

- Strict extraction/classification and other single-result workflows continue
  to use the one-native-call contract.
- Conversational prose remains fully natural, but LLM-authored prose is emitted
  through a native rendering tool and is never parsed into a financial mutation.
- Unknown, unavailable, malformed, or unsafe conversational tool sets still fail
  closed under ADR-033.
- Gateway telemetry records call kind and selected tool names without financial
  content.
- Deterministic callbacks and literal server-owned status/error acknowledgements
  remain valid without an LLM call.
- Go must not fill an intelligence outage with keyword/regex NLP, semantic
  switch-cases, or canned analytical reasoning. The safe fallback is to preserve
  deterministic functionality and fail/defer/review explicitly.
- If a proposed Go branch needs to understand human meaning, decide open-ended
  relevance/noteworthiness, or author analytical narrative, the implementer must
  first route that responsibility to Jev/generative intelligence or document an
  explicit ADR exception.
- A valid allowed expense category may auto-confirm at category confidence
  `0.85` or above when overall extraction confidence is at least `0.90`; lower
  confidence or an unknown category still routes to review.
