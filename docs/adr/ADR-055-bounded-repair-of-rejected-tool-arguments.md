# ADR-055: Rejected tool arguments get one bounded repair round

## Status

Accepted — 2026-10-13. Refines ADR-024 and ADR-033; Go remains the only validator
and mutation authority.

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
