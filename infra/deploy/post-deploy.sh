#!/bin/sh
set -eu

: "${RICHMOD_IMAGE_REGISTRY:?set RICHMOD_IMAGE_REGISTRY}"
: "${RICHMOD_IMAGE_TAG:?set RICHMOD_IMAGE_TAG}"
: "${RICHMOD_PREVIOUS_IMAGE_TAG:?set RICHMOD_PREVIOUS_IMAGE_TAG}"

health_url=${RICHMOD_HEALTH_URL:-https://finance.musaddiqr.com/healthz}
ready_url=${RICHMOD_READY_URL:-https://finance.musaddiqr.com/readyz}
docker_bin=${DOCKER_BIN:-docker}

curl --fail --silent --show-error "$health_url" >/dev/null
curl --fail --silent --show-error "$ready_url" >/dev/null

# Telegram does not follow redirects, so a stale registered webhook URL drops
# every inbound update while the health checks above stay green. Warn only: a
# webhook mismatch must not roll back an otherwise healthy deploy.
# The check needs WEB_ORIGIN and the bot credentials, which live only in the
# deployment environment file. Read just those three keys as literal KEY=value
# lines (the Docker env-file format, not shell), so the rest of the file is
# never evaluated; values already in the environment win.
env_file=${RICHMOD_ENV_FILE:-/opt/family-finance/finance.env}
env_value() {
	[ -r "$env_file" ] || return 0
	sed -n "s/^$1=//p" "$env_file" | tail -n 1
}
WEB_ORIGIN=${WEB_ORIGIN:-$(env_value WEB_ORIGIN)}
TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN:-$(env_value TELEGRAM_BOT_TOKEN)}
TELEGRAM_WEBHOOK_SECRET=${TELEGRAM_WEBHOOK_SECRET:-$(env_value TELEGRAM_WEBHOOK_SECRET)}
export WEB_ORIGIN TELEGRAM_BOT_TOKEN TELEGRAM_WEBHOOK_SECRET
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if ! sh "$script_dir/../../scripts/set-telegram-webhook.sh" --check; then
	printf '%s\n' "warning: telegram webhook --check failed; inbound Telegram updates are being dropped" >&2
	printf '%s\n' "warning: re-register with scripts/set-telegram-webhook.sh after confirming WEB_ORIGIN and the bot secret" >&2
fi

if [ "$RICHMOD_PREVIOUS_IMAGE_TAG" = "$RICHMOD_IMAGE_TAG" ]; then
	printf '%s\n' "current and previous image tags are identical; skipping image cleanup" >&2
	exit 0
fi

for repository in api worker migrate; do
	image_repository="$RICHMOD_IMAGE_REGISTRY/richmod-$repository"
	$docker_bin image ls "$image_repository" --format '{{.Repository}}:{{.Tag}}' |
	while IFS= read -r image; do
		[ -n "$image" ] || continue
		tag=${image##*:}
		case "$tag" in
			sha-*) ;;
			*) continue ;;
		esac
		if [ "$tag" = "$RICHMOD_IMAGE_TAG" ] || [ "$tag" = "$RICHMOD_PREVIOUS_IMAGE_TAG" ]; then
			continue
		fi
		if ! $docker_bin image rm "$image"; then
			printf '%s\n' "warning: could not remove unused release image $image" >&2
		fi
	done
done
