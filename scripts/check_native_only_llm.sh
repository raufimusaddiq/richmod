#!/usr/bin/env sh
set -eu

if rg -n '\.Structured\(|text\.format|json_schema' apps/worker apps/api/internal --glob '!**/*_test.go'; then
  echo 'native-only LLM guard failed' >&2
  exit 1
fi
