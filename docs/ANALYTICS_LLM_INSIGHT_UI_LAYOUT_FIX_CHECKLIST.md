# Analytics LLM Insight UI — Layout Fix Checklist

Execution record for deployed card spacing correction. Source plan remains unchanged.

- [x] Add explicit desktop card padding.
- [x] Add reduced mobile card padding.
- [x] Remove inherited header margin from grid gap to prevent doubled vertical spacing.
- [x] Add explicit separation from the main chart using the existing 14px page rhythm.
- [x] Refine the card as a restrained analyst note with a subtle accent, tinted surface, and stronger text hierarchy.
- [x] Keep failure text, retry behavior, and chart isolation unchanged.
- [x] Return documented `200 EXISTING` for any same-period pending insight, avoiding unique-index `409` errors.
- [x] Run web tests and production build.
- [x] Run API tests and vet: `go test ./internal/insight ./cmd/api`; `go vet ./internal/insight ./cmd/api` passed.
- [x] Deploy API and web from updated `main`; public `/` and `/analytics` return HTTP 200.
- [ ] Verify final card visually in authenticated desktop/mobile browser.

```text
Baseline main SHA: 46c097c3411dd532e43005956f908dc2df54a1aa
Branch: fix/analytics-insight-card-layout
Backend changes: Insight generation idempotency query only; no schema change.
Layout commit: 0d51ff3
API idempotency commit: 26b26ca
Visual refinement commit: cab2d3c
Deployed API image: sha256:2dbeab2190edf0947895bc6013ade5bf37fca38982e4af351f004e747944cb34
Deployed web image: sha256:daeb828fd344ba3d1463ccda8067b67c13380d69f251a0f01b3cd127e12c7596
```
