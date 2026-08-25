# ADR-005: Cloud LLM Gateway boundary

## Status

Accepted.

## Decision

All model calls route through the Cloud LLM Gateway. LLM output is untrusted,
strictly structured, and validated in Go before it can affect financial state.
