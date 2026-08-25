# ADR-001: Go backend

## Status

Accepted.

## Decision

Use Go services built on the standard library, `pgx/v5`, and explicit domain code.
Financial state transitions remain in Go and PostgreSQL is the canonical store.
