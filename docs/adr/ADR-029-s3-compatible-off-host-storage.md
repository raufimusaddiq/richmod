# ADR-029: S3-compatible off-host storage

## Status

Accepted — 2026-08-30

## Decision

Use one private S3-compatible bucket for Richmod off-host data, separated by
purpose beneath a configurable `OSS_PREFIX`:

```text
<prefix>/attachments/<household-id>/<opaque-name>
<prefix>/backups/restic/...
```

Document bytes are written to the local attachment volume and mirrored to S3
before PostgreSQL metadata is committed. Worker maintenance removes local bytes
after document processing reaches a terminal state and no document job is active.
Subsequent reads fetch directly from S3 and do not repopulate local storage. Object
names remain opaque and household-scoped; original filenames never become keys.

Database backups remain custom-format `pg_dump` archives inside encrypted restic
snapshots. Production derives its restic repository from the same OSS endpoint
and bucket but uses the separate `backups/restic` prefix. Restic encryption keys
remain independent from OSS credentials and require an off-host recovery copy.

Bucket remains private. Richmod issues no public object URLs or browser credentials.
After API session and household authorization, document-content endpoints respond
with a short-lived (two-minute), GET-only OSS presigned redirect. The signed URL
uses inline content response metadata, is never persisted or logged, and includes
`Referrer-Policy: no-referrer` on the redirect response.

## Consequences

- Host loss does not remove attachment evidence or database snapshots.
- Temporary OSS failure rejects new document uploads and prevents remote reads.
- Local bytes exist only while document extraction still needs them.
- API file bytes no longer relay through the application after authorization.
- Existing local attachments need one idempotent backfill before acceptance.
- Recovery acceptance requires an OSS round trip and disposable restore drill.
