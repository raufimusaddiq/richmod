#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat >"$tmp/curl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$CURL_LOG"
case "$*" in
  *getWebhookInfo*) printf '%s\n' '{"ok":true,"result":{"url":"https://finance.example.test/webhooks/telegram","pending_update_count":0}}' ;;
esac
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

# Without an environment file the webhook check cannot run and only warns.
env -u WEB_ORIGIN -u TELEGRAM_BOT_TOKEN -u TELEGRAM_WEBHOOK_SECRET \
RICHMOD_ENV_FILE="$tmp/missing.env" \
CURL_LOG="$tmp/curl.log" RM_LOG="$tmp/rm.log" PATH="$tmp:$PATH" \
RICHMOD_IMAGE_REGISTRY=ghcr.io/raufimusaddiq \
RICHMOD_IMAGE_TAG=sha-current RICHMOD_PREVIOUS_IMAGE_TAG=sha-previous \
DOCKER_BIN=docker RICHMOD_HEALTH_URL=http://health RICHMOD_READY_URL=http://ready \
"$root/post-deploy.sh" 2>"$tmp/stderr.log"

grep -q 'telegram webhook --check failed' "$tmp/stderr.log"
[ "$(wc -l <"$tmp/curl.log")" -eq 2 ]
[ "$(wc -l <"$tmp/rm.log")" -eq 3 ]
! grep -Eq 'sha-current|sha-previous' "$tmp/rm.log"

# With the deployment environment file, the check reads WEB_ORIGIN and the bot
# credentials from it (as literal values, never shell-evaluated) and passes.
cat >"$tmp/finance.env" <<'EOF'
POSTGRES_PASSWORD=$(touch should-not-run)
WEB_ORIGIN=https://finance.example.test
TELEGRAM_BOT_TOKEN=123:test-token
TELEGRAM_WEBHOOK_SECRET=test-secret
EOF
: >"$tmp/curl.log"
: >"$tmp/rm.log"
(cd "$tmp" && env -u WEB_ORIGIN -u TELEGRAM_BOT_TOKEN -u TELEGRAM_WEBHOOK_SECRET \
RICHMOD_ENV_FILE="$tmp/finance.env" \
CURL_LOG="$tmp/curl.log" RM_LOG="$tmp/rm.log" PATH="$tmp:$PATH" \
RICHMOD_IMAGE_REGISTRY=ghcr.io/raufimusaddiq \
RICHMOD_IMAGE_TAG=sha-current RICHMOD_PREVIOUS_IMAGE_TAG=sha-previous \
DOCKER_BIN=docker RICHMOD_HEALTH_URL=http://health RICHMOD_READY_URL=http://ready \
"$root/post-deploy.sh" 2>"$tmp/stderr-env.log" >"$tmp/stdout-env.log")

! grep -q 'telegram webhook --check failed' "$tmp/stderr-env.log"
grep -q 'telegram webhook ok: https://finance.example.test/webhooks/telegram' "$tmp/stdout-env.log"
grep -q 'getWebhookInfo' "$tmp/curl.log"
[ ! -e "$tmp/should-not-run" ]
