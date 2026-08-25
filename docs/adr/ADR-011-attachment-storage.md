# ADR-011: Attachment storage strategy

## Status

Accepted.

## Decision

Attachment bytes will be stored outside PostgreSQL; PostgreSQL stores validated
metadata, content hashes, and opaque storage references. The production storage
provider is configured at deployment time and receives a dedicated ADR if changed.

The current self-hosted production provider is a dedicated Docker volume mounted
read-write only by the API and read-only by the worker. Stored names are random
opaque references under a household-scoped directory; user filenames never form
storage paths. JPEG and PNG uploads are decoded, dimension-guarded, and re-encoded
to strip metadata before hashing and storage. PostgreSQL retains metadata and all
upload source events, while identical bytes share one immutable attachment.
