#!/usr/bin/env sh
set -eu

if git grep -nE '\.Structured\(|text\.format|json_schema' -- apps/worker apps/api/internal apps/reviewdomain ':!*_test.go'; then
  echo 'native-only LLM guard failed' >&2
  exit 1
fi

if git grep -nE 'write_financial_insight|noSignalResponse|json:"recommendation"|regexp\.|strings\.(Contains|EqualFold)|json\.(Unmarshal|NewDecoder).*([Tt]ext|[Pp]rose)' -- apps/worker/internal/insight ':!*_test.go'; then
  echo 'analytical native-tool/no-semantic-fallback guard failed' >&2
  exit 1
fi

if git grep -nE '\.(Exec|CopyFrom|SendBatch)\(' -- apps/reviewdomain/analyticscore ':!*_test.go' || git grep -nE 'https?://(api\.openai\.com|api\.anthropic\.com|generativelanguage\.googleapis\.com)' -- apps/worker/internal/insight apps/reviewdomain/analyticscore ':!*_test.go'; then
  echo 'analytical read-only/gateway boundary guard failed' >&2
  exit 1
fi
