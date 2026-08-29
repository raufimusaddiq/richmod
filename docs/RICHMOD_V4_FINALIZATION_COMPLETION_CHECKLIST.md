# Richmod V4 Finalization Completion Checklist

**Reference:** `RICHMOD_V4_FINALIZATION_HARDENING_REBASED.md` (unchanged)

**Branch:** `docs/v4-finalization-completion`

This is the execution record for the rebased hardening plan. It intentionally
does not alter the reference plan or reinterpret a check as complete without
code and verification evidence.

## Implemented hardening

- [x] P0.1 Web source reviews expose `COMPLETE_BANK_FACTS`, not generic
  confirmation. The API queues a bound completion job; the worker overlays
  only entered amount/timestamp facts, reuses the deterministic bank policy,
  preserves extraction evidence, and resolves the source item only after the
  canonical path completes. Replay detects an existing proposal.
- [x] P0.2 Bank persistence resolves or creates a canonical household merchant
  and stores it on `transaction.merchant_id`; raw merchant evidence remains on
  the proposal.
- [x] P0.3 Bank prompts explicitly allow observed `channel="TRANSFER"` while
  reserving ledger classification for Go.
- [x] P0.4 LLM bank evidence timestamps now require RFC3339/RFC3339Nano with
  an explicit zone; date-only and timezone-less values are rejected.
- [x] P1.1 Explicit, active, household-scoped merchant rules auto-apply their
  category only to eligible outgoing purchase-like events.
- [x] P1.2 New listener account keys are ID-derived rather than sender-derived;
  editing a sender no longer changes logical account identity. Migration 00030
  reconciles historical system keys without touching transaction attribution.
- [x] P1.3 Bank native tool calls omit optional `reasoning_effort` by default;
  configured protocol behavior remains explicit and single-protocol.
- [x] P1.4 Bank-created canonical transactions now receive compact
  `CREATE_FROM_BANK_EMAIL` worker audits without raw email content.
- [x] P1.5.5 Added targeted indexes for terminal job retention and
  case-insensitive merchant alias lookups; no cache or additional
  infrastructure was introduced.
- [x] P1.5.6 Worker maintenance prunes terminal jobs in locked batches:
  succeeded after 30 days and failed after 90 days.
- [x] P1.5.7 Review/audit paths retain compact references rather than raw bank
  evidence bodies.

## Required verification before V4 freeze

- [ ] Database-backed regression tests cover source completion (amount,
  timestamp, replay, cross-household, and resulting transaction review).
- [ ] Telegram source-review conversation states collect and bind bank facts to
  the stored review/message identity. Web completion is implemented; Telegram
  completion is not yet implemented in this branch.
- [ ] Run the complete API/worker/web suite plus disposable-PostgreSQL tests.
- [ ] Apply migration 00030 to a disposable upgraded database and inspect the
  migration/query plans.
- [ ] Production backup/restore rehearsal, controlled configured-bank test,
  and second-sender acceptance remain operational gates.

## Freeze decision

V4 must not be called frozen until every item in “Required verification before
V4 freeze” is checked. The code changes above improve the remaining hardening
path, but automated and controlled acceptance evidence is still required.
