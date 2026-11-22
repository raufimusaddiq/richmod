#!/bin/sh
set -eu

IMAGE=mcr.microsoft.com/playwright:v1.65.0-noble
BUN_VERSION=1.4.4
root=$(cd "$(dirname "$0")/../../.." && { pwd -W 2>/dev/null || pwd; })

case "${1:-}" in
  --update) shift; mode=--update-snapshots ;;
  *) mode= ;;
esac

export MSYS_NO_PATHCONV=1

inside='
npm install --global --prefix /tmp/richmod-bun bun@"$BUN_VERSION"
export PATH=/tmp/richmod-bun/bin:$PATH
bun install --frozen-lockfile --ignore-scripts
bun run build
status=0
npx playwright test --project=visual $MODE "$@" || status=$?
chown -R "$HOST_UID:$HOST_GID" out test-results playwright-report e2e/visual/baselines 2>/dev/null || true
exit $status
'

exec docker run --rm --ipc=host \
  -e HOME=/tmp/richmod-visual-home \
  -e BUN_VERSION="$BUN_VERSION" \
  -e MODE="$mode" \
  -e HOST_UID="$(id -u)" \
  -e HOST_GID="$(id -g)" \
  -v "$root:/src" -v /src/apps/web/node_modules -w /src/apps/web \
  "$IMAGE" sh -c "$inside" sh "$@"
