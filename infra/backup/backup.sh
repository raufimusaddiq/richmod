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

# A run killed mid-prune leaves an exclusive lock that blocks every later restic
# command. `unlock` removes only stale locks, never one held by a live process.
# Not fatal: it exits non-zero for a missing or unreachable repository, and the
# check below classifies those.
restic unlock 2>/dev/null || true

# Initialise only when restic says the repository is missing. Any other failure
# (network, credentials, lock) must surface as itself, not as a failed init.
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
restic backup --tag family-finance --tag daily "$@"
restic forget --tag family-finance --keep-daily 14 --keep-weekly 8 --keep-monthly 12 --prune
restic check --read-data-subset=5%
