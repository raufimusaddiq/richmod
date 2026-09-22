# ADR-005: Cloud AI Gateway boundary

## Status

Accepted — amended by ADR-030 and ADR-038.

## Decision

All external model inference routes through the Cloud AI Gateway (LiteRouter).
Model output is untrusted and must be validated by Go before it can affect
financial state.

The gateway exposes more than one native inference protocol:

```text
/v1/responses
/v1/chat/completions
    -> generative models

/v1/systemone
    -> bounded System One decision models such as Jev
```

Generative protocols remain appropriate for arbitrary extraction, vision,
reasoning, tool use, and prose. System One remains a distinct native protocol for
typed probabilistic decisions. Richmod must not translate System One into a fake
OpenAI-compatible chat model.

Finance services authenticate only to LiteRouter. Upstream provider credentials,
including the TypeSafe credential, remain owned by LiteRouter and are never
stored by Richmod.

For generative workflows, native tool calls and other model output remain
strictly decoded and validated before use.

For System One workflows, Richmod validates expected question keys, answer types,
probability ranges, configured policy thresholds, and current server state before
the result may influence a domain transition.

Logical model policy names are resolved by configuration; provider-specific
model names do not belong in financial business rules.

Deterministic ledger, authorization, binding, arithmetic, reconciliation, review,
and read flows must continue operating when the AI gateway is unavailable.
Model-dependent work may fail closed, ask for clarification, retry when safe, or
defer to Review Inbox according to the owning workflow.

Automatic protocol substitution is not allowed. In particular, a failed System
One decision must not silently fall back to a generative model answering the same
bounded question.
