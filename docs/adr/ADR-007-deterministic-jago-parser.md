# ADR-007: Deterministic Jago parser with LLM fallback

## Status

Accepted.

## Decision

Known Jago email templates are classified and parsed deterministically. An
unrecognized but trusted template is retained and may use structured LLM fallback
before Review Inbox handling; it is never blindly posted.
