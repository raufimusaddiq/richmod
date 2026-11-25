#!/bin/sh
set -eu

umask 077
: "${DATABASE_URL:?DATABASE_URL is required}"
: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY is required}"
: "${RESTIC_PASSWORD_FILE:?RESTIC_PASSWORD_FILE is required}"

if [ ! -r "$RESTIC_PASSWORD_FILE" ]; then
    echo "restic password file is not readable" >&2
    exit 1
fi

backup_host="family-finance"
backup_tmp="${TMPDIR:-/tmp}/family-finance-backup"

exec 9>"$backup_tmp.lock"
if ! flock -n 9; then
    echo "another backup run holds $backup_tmp.lock" >&2
    exit 1
fi

backup_files="database.dump created-at.txt"
clear_staging() {
    for name in $backup_files; do
        rm -f "$backup_tmp/$name"
    done
}
if [ -L "$backup_tmp" ] || { [ -e "$backup_tmp" ] && [ ! -d "$backup_tmp" ]; }; then
    echo "refusing backup: $backup_tmp is not a plain directory" >&2
    exit 1
fi
[ -d "$backup_tmp" ] || mkdir -m 700 "$backup_tmp"
clear_staging
trap clear_staging EXIT INT TERM

restic unlock 2>/dev/null || true

if ! probe_output="$(restic cat config 2>&1)"; then
    case "$probe_output" in
    *"does not exist"*|*"Is there a repository"*|*"no such file"*|*"NoSuchKey"*|*"NoSuchBucket"*)
        restic init
        ;;
    *)
        echo "restic repository check failed:" >&2
        echo "$probe_output" >&2
        exit 1
        ;;
    esac
fi

pg_dump --format=custom --no-owner --no-privileges --file="$backup_tmp/database.dump" "$DATABASE_URL"
pg_restore --list "$backup_tmp/database.dump" >/dev/null
date -u +%Y-%m-%dT%H:%M:%SZ > "$backup_tmp/created-at.txt"

set -- "$backup_tmp"
if [ -d /var/lib/finance/attachments ]; then
    set -- "$@" /var/lib/finance/attachments
fi
restic backup --host "$backup_host" --tag family-finance --tag daily "$@"
restic check --read-data-subset=5%
restic forget --tag family-finance --group-by tags --keep-daily 14 --keep-weekly 8 --keep-monthly 12 --prune
