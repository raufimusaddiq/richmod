#!/bin/sh
set -eu

umask 077
: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY is required}"
: "${RESTIC_PASSWORD_FILE:?RESTIC_PASSWORD_FILE is required}"
: "${RESTORE_DATABASE_URL:?RESTORE_DATABASE_URL is required}"

case "$RESTORE_DATABASE_URL" in
    */*_restore_drill|*/*_restore_drill\?*) ;;
    *)
        echo "refusing restore: target database name must end in _restore_drill" >&2
        exit 1
        ;;
esac

restore_tmp="$(mktemp -d)"
trap 'rm -rf "$restore_tmp"' EXIT INT TERM
restic restore latest --tag family-finance --target "$restore_tmp"
dump_path="$(find "$restore_tmp" -name database.dump -type f | head -n 1)"
if [ -z "$dump_path" ]; then
    echo "database.dump is missing from the latest snapshot" >&2
    exit 1
fi

pg_restore --exit-on-error --clean --if-exists --no-owner --no-privileges --dbname="$RESTORE_DATABASE_URL" "$dump_path"
psql "$RESTORE_DATABASE_URL" -v ON_ERROR_STOP=1 -Atc "SELECT 'restore-ok', count(*) FROM goose_db_version WHERE is_applied"
