# Analytics LLM Insight UI — Layout Fix Checklist

Execution record for deployed card spacing correction. Source plan remains unchanged.

- [x] Add explicit desktop card padding.
- [x] Add reduced mobile card padding.
- [x] Remove inherited header margin from grid gap to prevent doubled vertical spacing.
- [x] Keep failure text, retry behavior, and chart isolation unchanged.
- [x] Return documented `200 EXISTING` for any same-period pending insight, avoiding unique-index `409` errors.
- [x] Run web tests and production build.
- [x] Run API tests and vet: `go test ./internal/insight ./cmd/api`; `go vet ./internal/insight ./cmd/api` passed.
- [ ] Deploy API/web and verify authenticated desktop/mobile card.

```text
Baseline main SHA: 46c097c3411dd532e43005956f908dc2df54a1aa
Branch: fix/analytics-insight-card-layout
Backend changes: Insight generation idempotency query only; no schema change.
```
