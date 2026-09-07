#!/bin/sh
set -eu

: "${RICHMOD_IMAGE_REGISTRY:?set RICHMOD_IMAGE_REGISTRY}"
: "${RICHMOD_IMAGE_TAG:?set RICHMOD_IMAGE_TAG}"
: "${RICHMOD_PREVIOUS_IMAGE_TAG:?set RICHMOD_PREVIOUS_IMAGE_TAG}"

health_url=${RICHMOD_HEALTH_URL:-https://finance.investdx.biz.id/healthz}
ready_url=${RICHMOD_READY_URL:-https://finance.investdx.biz.id/readyz}
docker_bin=${DOCKER_BIN:-docker}

curl --fail --silent --show-error "$health_url" >/dev/null
curl --fail --silent --show-error "$ready_url" >/dev/null

if [ "$RICHMOD_PREVIOUS_IMAGE_TAG" = "$RICHMOD_IMAGE_TAG" ]; then
	printf '%s\n' "current and previous image tags are identical; skipping image cleanup" >&2
	exit 0
fi

for repository in api worker migrate web; do
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
