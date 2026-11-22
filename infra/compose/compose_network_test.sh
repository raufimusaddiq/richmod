#!/bin/sh
set -eu

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

prod_config=$(
	POSTGRES_DB=test POSTGRES_USER=test POSTGRES_PASSWORD=test \
	DATABASE_URL='postgres://test:test@db:5432/test?sslmode=disable' \
	SESSION_SECRET='12345678901234567890123456789012' \
	OSS_ENDPOINT='https://oss.example.test' OSS_BUCKET=test OSS_ACCESS_KEY=test OSS_SECRET_KEY=test OSS_REGION=ap-southeast-1 \
	docker compose -f "$root/compose.yaml" -f "$root/compose.production.yaml" config --format json
)

printf '%s' "$prod_config" | jq -e '.services.api.deploy.replicas == 1' >/dev/null
