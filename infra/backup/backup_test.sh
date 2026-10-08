#!/bin/sh
# Verifies backup.sh only runs `restic init` for a missing repository and
# surfaces every other restic failure instead of masking it.
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM
mkdir "$work/bin"
echo pw > "$work/pw"

cat > "$work/bin/restic" <<'STUB'
#!/bin/sh
echo "$1" >> "$STUB_LOG"
if [ "$1" = unlock ]; then
    case "$STUB_REPO" in
    missing) exit 10 ;;
    broken) exit 1 ;;
    esac
fi
if [ "$1" = cat ]; then
    case "$STUB_REPO" in
    missing) echo "Fatal: unable to open config file: Stat: file does not exist" >&2; exit 10 ;;
    broken) echo "Fatal: unable to open repository: dial tcp: i/o timeout" >&2; exit 1 ;;
    esac
fi
exit 0
STUB
cat > "$work/bin/pg_dump" <<'STUB'
#!/bin/sh
for a in "$@"; do case "$a" in --file=*) : > "${a#--file=}" ;; esac; done
STUB
printf '#!/bin/sh\nexit 0\n' > "$work/bin/pg_restore"
chmod +x "$work/bin"/*

run() {
    : > "$work/log"
    STUB_LOG="$work/log" STUB_REPO="$1" PATH="$work/bin:$PATH" \
        DATABASE_URL=postgres://x RESTIC_REPOSITORY=s3:x RESTIC_PASSWORD_FILE="$work/pw" \
        sh "$here/backup.sh" >"$work/out" 2>&1
}

fail() { echo "FAIL: $1" >&2; cat "$work/out" >&2; exit 1; }

run existing || fail "existing repository should back up"
grep -qx init "$work/log" && fail "existing repository must not be re-initialised"
grep -qx backup "$work/log" || fail "existing repository should run backup"

run missing || fail "missing repository should be initialised then backed up"
grep -qx init "$work/log" || fail "missing repository should be initialised"
grep -qx backup "$work/log" || fail "missing repository should run backup"

if run broken; then fail "broken repository access must fail"; fi
grep -qx init "$work/log" && fail "broken repository access must not run init"
grep -q "i/o timeout" "$work/out" || fail "real restic error must be shown"

grep -qx unlock "$work/log" || fail "stale locks should be cleared before the repository check"

echo "backup bootstrap tests passed"
