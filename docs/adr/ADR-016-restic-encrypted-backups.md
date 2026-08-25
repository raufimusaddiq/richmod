# ADR-016: Restic for encrypted production backups

## Status

Accepted — 2026-08-25

## Decision

Use a dedicated, one-shot backup container containing PostgreSQL 17 client tools
and restic. Each run creates a custom-format `pg_dump`, verifies that archive,
and backs it up together with attachment storage. Restic provides encryption,
repository integrity checks, off-host repository support, and retention pruning.

The restic password is mounted from a root-readable host file and is never
committed. Production must set `RESTIC_REPOSITORY` to an off-host repository.
The supported default retention is 14 daily, 8 weekly, and 12 monthly snapshots.

A restore drill targets only a disposable database whose name ends in
`_restore_drill`. This guard prevents accidental replacement of the canonical
production database.

## Consequences

- PostgreSQL and attachment evidence are captured together in one encrypted
  snapshot.
- The scheduler is a host systemd timer; it does not introduce another queue or
  application runtime.
- Production acceptance still requires off-host repository credentials and a
  successful restore drill after those credentials are installed.
