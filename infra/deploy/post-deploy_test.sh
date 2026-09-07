#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat >"$tmp/curl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$CURL_LOG"
EOF
cat >"$tmp/docker" <<'EOF'
#!/bin/sh
case "$*" in
  "image ls "*)
    repository=$3
    printf '%s\n' \
      "$repository:sha-current" \
      "$repository:sha-previous" \
      "$repository:sha-old"
    ;;
  "image rm "*) printf '%s\n' "$3" >>"$RM_LOG" ;;
esac
EOF
chmod +x "$tmp/curl" "$tmp/docker"

CURL_LOG="$tmp/curl.log" RM_LOG="$tmp/rm.log" PATH="$tmp:$PATH" \
RICHMOD_IMAGE_REGISTRY=ghcr.io/raufimusaddiq \
RICHMOD_IMAGE_TAG=sha-current RICHMOD_PREVIOUS_IMAGE_TAG=sha-previous \
DOCKER_BIN=docker RICHMOD_HEALTH_URL=http://health RICHMOD_READY_URL=http://ready \
"$root/post-deploy.sh"

[ "$(wc -l <"$tmp/curl.log")" -eq 2 ]
[ "$(wc -l <"$tmp/rm.log")" -eq 4 ]
! grep -Eq 'sha-current|sha-previous' "$tmp/rm.log"
