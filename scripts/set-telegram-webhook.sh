#!/bin/sh
# Register or verify this deployment's Telegram webhook.
#
#   WEB_ORIGIN=https://finance.example.com \
#   TELEGRAM_BOT_TOKEN=... TELEGRAM_WEBHOOK_SECRET=... \
#   scripts/set-telegram-webhook.sh [--check]
#
# Telegram does not follow redirects, so a stale registered URL silently drops
# every inbound update while /healthz and /readyz stay green. `--check` exits
# non-zero when the registered URL differs from ${WEB_ORIGIN}/webhooks/telegram.
# Never echoes the token or the secret.
set -eu

mode=set
case "${1-}" in
	--check) mode=check ;;
	"") ;;
	*)
		printf 'usage: %s [--check]\n' "$0" >&2
		exit 2
		;;
esac

: "${WEB_ORIGIN:?WEB_ORIGIN must be set to the public origin, e.g. https://finance.example.com}"
: "${TELEGRAM_BOT_TOKEN:?TELEGRAM_BOT_TOKEN must be set}"
: "${TELEGRAM_WEBHOOK_SECRET:?TELEGRAM_WEBHOOK_SECRET must be set}"

webhook_url=${WEB_ORIGIN%/}/webhooks/telegram
api_url=https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}

# The bot token is part of the API URL, so curl's error text is filtered too.
redact() {
	sed -e "s|${TELEGRAM_BOT_TOKEN}|<redacted>|g" -e "s|${TELEGRAM_WEBHOOK_SECRET}|<redacted>|g"
}

if [ "$mode" = set ]; then
	if ! raw=$(printf '%s' "$TELEGRAM_WEBHOOK_SECRET" | curl --silent --show-error --max-time 20 \
		--request POST "$api_url/setWebhook" \
		--data-urlencode "url=$webhook_url" \
		--data-urlencode 'allowed_updates=["message","callback_query"]' \
		--data-urlencode 'secret_token@-' 2>&1); then
		printf 'setWebhook request failed: %s\n' "$(printf '%s\n' "$raw" | redact)" >&2
		exit 1
	fi
	response=$(printf '%s\n' "$raw" | redact)
	case "$response" in
		'{"ok":true'*) printf 'setWebhook ok: %s\n' "$webhook_url" ;;
		*)
			printf 'setWebhook rejected: %s\n' "$response" >&2
			exit 1
			;;
	esac
fi

if ! raw=$(curl --silent --show-error --max-time 20 "$api_url/getWebhookInfo" 2>&1); then
	printf 'getWebhookInfo request failed: %s\n' "$(printf '%s\n' "$raw" | redact)" >&2
	exit 1
fi
response=$(printf '%s\n' "$raw" | redact)
registered=$(printf '%s\n' "$response" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p' | head -n 1)

if [ "$registered" != "$webhook_url" ]; then
	printf 'telegram webhook mismatch: registered %s, expected %s\n' \
		"${registered:-<none>}" "$webhook_url" >&2
	printf '%s\n' "$response" >&2
	exit 1
fi

printf 'telegram webhook ok: %s\n' "$webhook_url"
