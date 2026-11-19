#!/bin/sh
# Visual baselines are canonical to the pinned Playwright image, not to whatever
# Chromium a developer host happens to have: the pinned self-hosted woff2 fonts
# are byte-identical everywhere, but Chromium's FreeType/fontconfig rasterises
# their glyph edges differently per host, which is enough to move every baseline
# past the 0.5% threshold. Same reason the README capture runbook uses an image.
set -eu

IMAGE=mcr.microsoft.com/playwright:v1.65.0-noble
root=$(cd "$(dirname "$0")/../../.." && pwd)

# Already inside the pinned image (e.g. CI): run the smoke directly, no nested docker.
if [ "${RICHMOD_VISUAL_IMAGE:-}" = "1" ]; then
  exec node scripts/visual-smoke.mjs "$@"
fi

# Share the caller's uid so build output and regenerated baselines stay owned by
# the user; HOME/TMPDIR keep npm's cache and Chromium's profile out of /root.
exec docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e HOME=/tmp/richmod-visual-home \
  -e TMPDIR=/tmp \
  -e RICHMOD_VISUAL_IMAGE=1 \
  -e NEXT_TELEMETRY_DISABLED=1 \
  -e UPDATE_VISUAL_BASELINES="${UPDATE_VISUAL_BASELINES:-0}" \
  -v "$root:/src" -w /src/apps/web \
  "$IMAGE" sh -c 'npm ci --ignore-scripts --no-audit --no-fund && npm run build && exec node scripts/visual-smoke.mjs "$@"' sh "$@"
