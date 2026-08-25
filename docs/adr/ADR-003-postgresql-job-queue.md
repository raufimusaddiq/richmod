# ADR-003: PostgreSQL-backed job queue

## Status

Accepted.

## Decision

Background jobs will live in PostgreSQL and be claimed using row locks with
`FOR UPDATE SKIP LOCKED`. No additional queue service is introduced.
