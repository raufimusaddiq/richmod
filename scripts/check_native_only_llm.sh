#!/usr/bin/env sh
set -eu

command -v rg >/dev/null 2>&1 || { echo 'ripgrep is required for native-only LLM guard' >&2; exit 1; }

if rg -n '\.Structured\(|text\.format|json_schema' apps/worker apps/api/internal apps/reviewdomain --glob '!**/*_test.go'; then
  echo 'native-only LLM guard failed' >&2
  exit 1
fi

# Analytics prose may only come from native rendering arguments, never a text
# parser, retired recommendation DTO, or Go pseudo-analyst fallback.
if rg -n 'write_financial_insight|noSignalResponse|json:"recommendation"|regexp\.|strings\.(Contains|EqualFold)|json\.(Unmarshal|NewDecoder).*([Tt]ext|[Pp]rose)' apps/worker/internal/insight --glob '!**/*_test.go'; then
  echo 'analytical native-tool/no-semantic-fallback guard failed' >&2
  exit 1
fi

# The shared analytical engine cannot acquire a financial write capability.
if rg -n '\.(Exec|CopyFrom|SendBatch)\(' apps/reviewdomain/analyticscore --glob '!**/*_test.go' || rg -n 'https?://(api\.openai\.com|api\.anthropic\.com|generativelanguage\.googleapis\.com)' apps/worker/internal/insight apps/reviewdomain/analyticscore --glob '!**/*_test.go'; then
  echo 'analytical read-only/gateway boundary guard failed' >&2
  exit 1
fi
