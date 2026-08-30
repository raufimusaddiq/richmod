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

Document bytes are written to the existing local attachment volume and mirrored
to S3 before PostgreSQL metadata is committed. Reads prefer the local cache and
fall back to S3, repopulating the cache after a successful remote read. Object
names remain opaque and household-scoped; original filenames never become keys.

Database backups remain custom-format `pg_dump` archives inside encrypted restic
snapshots. Production derives its restic repository from the same OSS endpoint
and bucket but uses the separate `backups/restic` prefix. Restic encryption keys
remain independent from OSS credentials and require an off-host recovery copy.

Bucket remains private. Richmod issues no public object URLs or browser credentials.

## Consequences

- Host loss does not remove attachment evidence or database snapshots.
- Temporary OSS failure rejects new document uploads, preventing false durability.
- Existing local attachments need one idempotent backfill before acceptance.
- Recovery acceptance requires an OSS round trip and disposable restore drill.
