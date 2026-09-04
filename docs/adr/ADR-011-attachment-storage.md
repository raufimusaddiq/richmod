# ADR-011: Attachment storage strategy

## Status

Accepted.

## Decision

Attachment bytes will be stored outside PostgreSQL; PostgreSQL stores validated
metadata, content hashes, and opaque storage references. The production storage
provider is configured at deployment time and receives a dedicated ADR if changed.

ADR-029 extends this decision with an S3-compatible off-host mirror. The Docker
volume remains a local cache mounted read-write by the API and document worker.
The worker writes only when staging an
authenticated Telegram image into the generic document pipeline. Stored names are
random opaque references under a household-scoped directory; user filenames never
form storage paths. JPEG and PNG uploads are decoded, dimension-guarded, and
re-encoded to strip metadata before hashing and storage. PostgreSQL retains
metadata and all upload source events, while identical bytes share one immutable
attachment.

Telegram image staging applies the same bounded JPEG/PNG MIME, extension,
dimension, pixel-count, metadata-stripping, opaque-name, and content-hash controls
before creating the existing `document` record and `PROCESS_DOCUMENT` job. When
OSS is configured, a new attachment is accepted only after its mirror succeeds.
