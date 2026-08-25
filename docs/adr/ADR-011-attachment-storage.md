# ADR-011: Attachment storage strategy

## Status

Accepted.

## Decision

Attachment bytes will be stored outside PostgreSQL; PostgreSQL stores validated
metadata, content hashes, and opaque storage references. The production storage
provider is configured at deployment time and receives a dedicated ADR if changed.
