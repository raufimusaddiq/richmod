# Analytics Current-Day and Category Ranking Fix — Checklist

Execution record for production bugs reported on August 30, 2026.

## Facts

- [x] Production contains one confirmed August 30 expense for IDR 70,129.
- [x] Production daily SQL returns August 24 through August 30, including that expense.
- [x] Both Ringkasan and Analisis trim cycle rows using API `daysElapsed`.
- [x] `Peringkat kategori` generated the synthetic `Lainnya` row in frontend code; no ledger transaction used that category.

## Fix

- [x] Calculate elapsed cycle days from Jakarta calendar dates through the injectable handler clock.
- [x] Keep cycle-day count clamped to the deterministic cycle bounds.
- [x] Keep Ringkasan donut compaction unchanged.
- [x] Render all positive real categories in `Peringkat kategori`, without synthetic `Lainnya`.
- [x] Grow category ranking height when more than eight categories exist.
- [x] Add focused Go and JavaScript regression tests.

## Verification

- [x] API analytics tests and vet pass.
- [x] Web tests pass (31 tests).
- [x] Production web build passes.
- [ ] Feature branch pushed and merged with `--no-ff`.
- [ ] Deployment performed only if explicitly requested.
