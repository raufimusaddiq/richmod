# V4 Telegram Message Polish — Completion Checklist

## Scope

Prettify Telegram bot messages with structured headings, emoji markers, inclusive Jakarta
period labels, and consistent IDR formatting. No callback/payload changes; plain-text
messages only.

## Changes

- [x] Review questions show `Nominal` / `Merchant` labels instead of arrow format
- [x] Missing-merchant review prompts use yellow-dot heading
- [x] Missing-purpose review prompts use yellow-dot heading
- [x] Transaction confirmations show `✅ Pemasukan/Pengeluaran tercatat` with label + amount
- [x] Cash-flow reply uses `💰 Arus kas` heading with resolved Jakarta period
- [x] Spending reply uses `💸 Pengeluaran` heading with period label
- [x] Search reply uses `🔎 Hasil pencarian` with keyword + period context
- [x] Insight reply uses `📊 Insight keuangan` / `📊 Ringkasan keuangan` headings
- [x] Review list reply uses `🟡 Review terbuka` / `✅ Tidak ada review` headings
- [x] Correction success uses `✅ Transaksi diperbarui` heading
- [x] Bank review notification includes nominal + description context
- [x] Payslip prompt lists employer + net pay with structured choice instructions
- [x] `assistantRange.label()` returns inclusive Jakarta date range (last day of period)

## Tests

- [x] `TestReviewQuestionUsesIndonesianIDRFormat` updated for new format
- [x] `TestAssistantRangeLabelUsesInclusiveJakartaDates` added
- [x] `TestReviewInitialStateCollectsMissingFactsBeforeCategory` updated for new wording
- [x] All worker packages pass (`go test ./...` and `go vet ./...`)

## Deployment

- Branch: `feat/telegram-message-polish`
- Commit: `00211fd`
- Merged to `main`: `e7ef9f6`
- Worker image rebuilt and restarted: `family-finance-worker-1` running

## Verification

- [x] Worker starts and logs "worker started" successfully
- [x] All family-finance services healthy (API 200, DB healthy, web up, worker up)

## Files Changed

- `apps/worker/internal/telegram/review.go` — review question + initial state prompts
- `apps/worker/internal/telegram/assistant.go` — range labels + all assistant reply formatting
- `apps/worker/internal/telegram/processor.go` — transaction confirmation message
- `apps/worker/internal/telegram/review_start_test.go` — expected wording update
- `apps/worker/internal/telegram/review_test.go` — IDR format + range label tests
- `apps/worker/internal/document/payslip.go` — payslip review prompts
- `apps/worker/internal/bankemail/processor.go` — bank review notification
