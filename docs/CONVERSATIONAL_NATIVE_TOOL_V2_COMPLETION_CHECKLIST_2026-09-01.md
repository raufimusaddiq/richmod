# Conversational Native Tool V2 — Completion Tracking

Source of truth: `docs/RICHMOD_CONVERSATIONAL_NATIVE_TOOL_V2.md` (unchanged).

## Implemented in this iteration

- [x] Migration `00040_telegram_turn_references.sql` adds scoped, expiring server-private transaction references.
- [x] Search results persist bounded `tx_1`…`tx_n` references without exposing canonical UUIDs.
- [x] Correction tool accepts `target_ref`; Go resolves household/user/chat/expiry/entity scope.
- [x] Native correction stages category, description, and optional timestamp changes for confirmation.
- [x] Pending correction confirmation applies all staged fields atomically and audits the edit.
- [x] Conditional `resolve_salary_choice` native tool dispatches to existing transactional salary resolver.
- [x] Conditional `resolve_merchant_learning` native tool dispatches to existing audited merchant-rule resolver.

## Remaining before V2 release gate

- [ ] Expand `resolve_review` action matrix and descriptors for every review type, including required-field validation parity with web.
- [ ] Add DB-backed end-to-end tests for reference scope/expiry, salary choice, merchant learning, review binding, and CHAT lane routing.
- [ ] Add assistant/tool turn persistence assertions covering delivery status and bounded public context.
- [ ] Run disposable PostgreSQL migration through version 40 and full API/worker CI.
- [ ] Production rollout, smoke, and rollback observation. Not run in this iteration.

## Verification completed

- `go test ./...` in `apps/worker` — passed in `golang:1.24.1-alpine`.
- `gofmt` and `git diff --check` — passed.

## Notes

- Backend architecture, LLM protocol, prompts, financial schema semantics, and source-of-truth PRD were not changed.
- Migration is additive; no canonical financial records are deleted.
