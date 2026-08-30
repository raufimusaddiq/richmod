# Off-host storage completion checklist

Baseline: `3545de2`

## Application storage

- [x] Private S3-compatible configuration uses `OSS_*` environment variables.
- [x] Attachment keys use `richmod/attachments/<household>/<opaque-name>`.
- [x] Web and Telegram uploads mirror normalized bytes before DB commit.
- [x] API and worker reads fall back to OSS when local cache is absent.
- [x] Browser receives no OSS credential or public object URL.
- [x] Partial OSS configuration fails application startup.
- [x] Existing production attachment volume backfilled idempotently.

## Backup and recovery

- [x] Production restic repository resolves under `richmod/backups/restic`.
- [x] PostgreSQL dump stays custom-format and passes `pg_restore --list`.
- [x] Attachment cache remains inside encrypted snapshots.
- [x] Restore refuses targets not ending in `_restore_drill`.
- [x] Live encrypted backup uploaded to provisioned OSS (`d30d4eb6`).
- [x] Live snapshot restored into disposable PostgreSQL 17.
- [ ] Production backup timer succeeds against OSS after code deployment.

## Verification

- [x] API full test suite and vet pass with disposable PostgreSQL.
- [x] Worker full test suite and vet pass with disposable PostgreSQL.
- [x] Production API, worker, and backup images build.
- [x] Live OSS attachment put/get/delete smoke passes.
- [x] Production Compose config resolves OSS settings.
- [ ] Feature branch committed and pushed.
- [ ] Merged to `main` with `--no-ff` and pushed.
- [ ] Production deployed only after explicit approval.
