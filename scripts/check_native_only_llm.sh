#!/usr/bin/env sh
set -eu

command -v rg >/dev/null 2>&1 || { echo 'ripgrep is required for native-only LLM guard' >&2; exit 1; }

if rg -n '\.Structured\(|text\.format|json_schema' apps/worker apps/api/internal --glob '!**/*_test.go'; then
  echo 'native-only LLM guard failed' >&2
  exit 1
fi
