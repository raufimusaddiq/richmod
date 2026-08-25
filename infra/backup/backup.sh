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

backup_tmp="$(mktemp -d)"
trap 'rm -rf "$backup_tmp"' EXIT INT TERM

if ! restic snapshots >/dev/null 2>&1; then
    restic init
fi

pg_dump --format=custom --no-owner --no-privileges --file="$backup_tmp/database.dump" "$DATABASE_URL"
pg_restore --list "$backup_tmp/database.dump" >/dev/null
date -u +%Y-%m-%dT%H:%M:%SZ > "$backup_tmp/created-at.txt"

set -- "$backup_tmp"
if [ -d /var/lib/finance/attachments ]; then
    set -- "$@" /var/lib/finance/attachments
fi
restic backup --tag family-finance --tag daily "$@"
restic forget --tag family-finance --keep-daily 14 --keep-weekly 8 --keep-monthly 12 --prune
restic check --read-data-subset=5%
