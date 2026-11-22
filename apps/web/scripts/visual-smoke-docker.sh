#!/bin/sh
set -eu

IMAGE=mcr.microsoft.com/playwright:v1.65.0-noble
root=$(cd "$(dirname "$0")/../../.." && pwd)

if [ "${RICHMOD_VISUAL_IMAGE:-}" = "1" ]; then
  exec node scripts/visual-smoke.mjs "$@"
fi

exec docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e HOME=/tmp/richmod-visual-home \
  -e TMPDIR=/tmp \
  -e RICHMOD_VISUAL_IMAGE=1 \
  -e NEXT_TELEMETRY_DISABLED=1 \
  -e UPDATE_VISUAL_BASELINES="${UPDATE_VISUAL_BASELINES:-0}" \
  -v "$root:/src" -w /src/apps/web \
  "$IMAGE" sh -c 'npm install --global --prefix /tmp/richmod-bun bun@1.4.4 && PATH=/tmp/richmod-bun/bin:$PATH && bun install --frozen-lockfile --ignore-scripts && bun run build && exec node scripts/visual-smoke.mjs "$@"' sh "$@"
