#!/bin/sh
set -eu

: "${RICHMOD_GIT_SHA:?set RICHMOD_GIT_SHA}"
: "${RICHMOD_IMAGE_TAG:?set RICHMOD_IMAGE_TAG}"
: "${RICHMOD_IMAGE_REGISTRY:?set RICHMOD_IMAGE_REGISTRY}"
: "${GHCR_USERNAME:?set GHCR_USERNAME}"

if [ -n "$(git status --porcelain)" ]; then
  echo "deployment worktree is not clean" >&2
  exit 1
fi

git fetch origin main
git checkout main
git pull --ff-only origin main

if [ "$(git rev-parse HEAD)" != "$RICHMOD_GIT_SHA" ]; then
  echo "main does not match requested release SHA" >&2
  exit 1
fi

IFS= read -r ghcr_token
if [ -z "$ghcr_token" ]; then
  echo "missing registry token" >&2
  exit 1
fi
printf '%s' "$ghcr_token" | docker login ghcr.io -u "$GHCR_USERNAME" --password-stdin >/dev/null
trap 'docker logout ghcr.io >/dev/null 2>&1 || true' EXIT

export RICHMOD_API_IMAGE="$RICHMOD_IMAGE_REGISTRY/richmod-api:$RICHMOD_IMAGE_TAG"
export RICHMOD_WORKER_IMAGE="$RICHMOD_IMAGE_REGISTRY/richmod-worker:$RICHMOD_IMAGE_TAG"
export RICHMOD_WEB_IMAGE="$RICHMOD_IMAGE_REGISTRY/richmod-web:$RICHMOD_IMAGE_TAG"
export RICHMOD_MIGRATE_IMAGE="$RICHMOD_IMAGE_REGISTRY/richmod-migrate:$RICHMOD_IMAGE_TAG"

compose='docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml'
$compose config --quiet
$compose pull migrate api worker web
$compose up -d --no-build migrate
$compose wait migrate
$compose up -d --no-build --wait api worker web
curl --fail --silent --show-error https://finance.investdx.biz.id/healthz >/dev/null
curl --fail --silent --show-error https://finance.investdx.biz.id/readyz >/dev/null
