#!/bin/sh
set -eu

# The local stack publishes the API on loopback but the API still makes outbound
# requests (OSS uploads, the LLM gateway). At least one of its Compose networks
# must therefore not be internal; otherwise `docker compose up` has no egress.
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)

config=$(
	POSTGRES_DB=test POSTGRES_USER=test POSTGRES_PASSWORD=test \
	DATABASE_URL='postgres://test:test@db:5432/test?sslmode=disable' \
	SESSION_SECRET='12345678901234567890123456789012' \
	docker compose -f "$root/compose.yaml" config --format json
)

printf '%s' "$config" | jq -e '
	. as $root
	| ($root.services.api.networks | keys) as $names
	| any($names[]; (($root.networks[.] // {}) | .internal) != true)
' >/dev/null
