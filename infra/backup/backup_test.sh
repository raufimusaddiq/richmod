#!/bin/sh
# Verifies backup.sh only runs `restic init` for a missing repository and
# surfaces every other restic failure instead of masking it.
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM
mkdir "$work/bin" "$work/tmp"
echo pw > "$work/pw"

cat > "$work/bin/restic" <<'STUB'
#!/bin/sh
echo "$1" >> "$STUB_LOG"
echo "$*" >> "$STUB_LOG.args"
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
    : > "$work/log.args"
    STUB_LOG="$work/log" STUB_REPO="$1" PATH="$work/bin:$PATH" \
        TMPDIR="$work/tmp" DATABASE_URL=postgres://x RESTIC_REPOSITORY=s3:x RESTIC_PASSWORD_FILE="$work/pw" \
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

# Retention only works when consecutive runs land in the same forget group and
# can find a parent: host and backed-up paths must be identical across runs.
run existing || fail "first run should back up"
grep '^backup ' "$work/log.args" > "$work/first"
run existing || fail "second run should back up"
grep '^backup ' "$work/log.args" > "$work/second"
cmp -s "$work/first" "$work/second" || fail "consecutive backups must use the same host and paths: $(cat "$work/first") vs $(cat "$work/second")"
grep -q -- '--host family-finance' "$work/first" || fail "backup must pin its host"
grep '^forget ' "$work/log.args" | grep -q -- '--group-by tags' || fail "forget must group by tags"

# A second run while one holds the lock must refuse, not clear the staged dump.
flock "$work/tmp/family-finance-backup.lock" sleep 3 &
holder=$!
sleep 1
echo keep > "$work/tmp/family-finance-backup/database.dump"
if run existing; then fail "a concurrent run must refuse"; fi
grep -q "holds" "$work/out" || fail "concurrent refusal must name the lock"
[ -s "$work/tmp/family-finance-backup/database.dump" ] || fail "a refused run must not clear the running run's dump"
# Let the holder release on its own: killing flock would leave sleep holding it.
wait "$holder"

# Only the script's own files are cleared, and a symlinked staging path is refused.
run existing || fail "run after lock release should back up"
echo other > "$work/tmp/family-finance-backup/unrelated"
run existing || fail "run with a foreign file should back up"
[ -f "$work/tmp/family-finance-backup/unrelated" ] || fail "foreign files in the staging dir must not be deleted"
mkdir "$work/elsewhere"
rm -f "$work/tmp/family-finance-backup/unrelated"
rmdir "$work/tmp/family-finance-backup"
ln -s "$work/elsewhere" "$work/tmp/family-finance-backup"
if run existing; then fail "a symlinked staging path must be refused"; fi
grep -q "not a plain directory" "$work/out" || fail "symlink refusal must be explained"

echo "backup bootstrap tests passed"
