# ADR-055: Rejected tool arguments get one bounded repair round

## Status

Accepted — 2026-10-13. Refines ADR-024 and ADR-033; Go remains the only validator
and mutation authority. Amended 2026-11-05 (date/time rules in validation,
executor refusals).

## Context

When Go rejected a tool call's arguments (for example an empty `target_ref`, an
amount that is not whole IDR, or a limit out of range), the conversational agent
ended the turn with a fixed apology. The model was never told why, so it could not
fix a one-field mistake or ask the user for the missing detail, and the rejection
was not recorded anywhere. Read-side expiry already works the other way: an expired
category ref becomes a tool result the model acts on (`recoverableAnalyticsRead`).

Production measurements (2026-09-02 to 2026-10-13, counts only) show this dead end
is rare: none of the 111 assistant replies was one of the fixed refusal messages, and
one `resolve_review` call ended `MISSING_REVIEW_DETAIL`. The 14 failed `AGENT_TEXT`
calls all stopped at the old 8 s model cap; the cap is now 25 s and the last failure
was 2026-10-02. Ambiguous reply bindings were 4 of 8 CEU bindings. So this change is a
safeguard for a dead end that was Go's, not a fix for a measured problem; the repair
log line is how its real rate will be known.

## Decision

1. A validation failure of a tool call's **arguments** (`agentArgumentsError`) is
   returned to the model once per turn as the call's tool output:
   `INVALID_ARGUMENTS` with Go's reason (truncated to 160 characters) and an
   instruction to correct the arguments or ask the user, never to guess. Other
   calls in the same response get `NOT_EXECUTED`, because the provider needs one
   output per call.
2. Go validates the corrected call exactly as before. Nothing ran on the rejected
   call, so no state changed and no authority was added.
3. Only argument errors are repairable. An unavailable tool, an unknown tool, a
   budget, a mixed call set or a second side effect is a server-state decision and
   stays final with the existing fixed reply.
4. The bound is one repair per turn (`maxArgumentRepairs`) and the existing model
   phase budget. A second rejection ends the turn as before.
5. Each repair logs the tool name and repair count (no values) so the rate can be
   measured.

## Consequences

- A malformed field no longer costs the user the whole turn.
- One more model call in the rejected case, inside the existing phase and time
  limits.
- Reasons come from Go's own validation messages and may echo a value the model
  itself produced; they contain no canonical IDs.
- Ambiguous reply bindings are not changed here.

## Amendment — date/time rules and executor refusals (2026-11-05)

Production showed model-fixable mistakes still failing the job: an Indonesian
local time such as `15.00`, a missing `date_provenance`, an unparseable
`explicit_date`, an invalid `item_ref`, a salary-cycle period with no salary
cycle, a custom range over 366 days. They surfaced inside executors, after
validation, as job errors that burned retries.

- Every date/time rule `nativeValidatedExtraction` applies is now checked first in
  `validateCreate` (and the shared `validateDateFields` for corrections and
  transfers), so the one repair round above covers them. `15.00`, `9:30` and
  `15:00` are all valid local times. The batch `item_ref` shape is validated
  (`batch_1`..`batch_10`).
- Validation messages name the expected format, so the reason returned to the
  model is bounded at 240 bytes (was 160).
- Refusals that can only be known while executing (an `item_ref` outside the
  pending batch, an unknown category slug, an unservable period, an unparseable
  correction date) are `agentRecoverable` results: returned as the call's tool
  output with `status`, `reason` and `next_step`, at most two per turn, with
  Indonesian fallback copy if the turn runs out of budget. Nothing runs on a
  refused call. READ-side equivalents (`CYCLE_UNAVAILABLE`,
  `RANGE_OUT_OF_BOUNDS`, `INVALID_RANGE`, `TARGET_UNAVAILABLE`) follow the
  `recoverableAnalyticsRead` pattern.
