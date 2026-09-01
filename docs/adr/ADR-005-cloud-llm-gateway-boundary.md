# ADR-005: Cloud LLM Gateway boundary

## Status

Accepted — amended by ADR-030.

## Decision

All model calls route through the Cloud LLM Gateway. LLM output is untrusted,
strictly structured, and validated in Go before it can affect financial state.

The deployed gateway exposes an OpenAI-compatible Responses API. Finance services
use bearer authentication, explicitly request non-streaming responses, require
required native tool calls, propagate a correlation ID, and record returned model
and usage metadata. Logical model policy names are resolved by configuration; no
provider model name belongs in financial business rules.

Gateway configuration remains optional at process startup so deterministic ledger
and review flows continue operating during a gateway outage or before credentials
are provisioned. Only LLM-dependent jobs may fail or defer to review.
