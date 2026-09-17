# ADR-037: Bounded intelligent document interpretation

## Status

Accepted for staged implementation. Existing extraction remains the primary
financial-state path until the documented rollout gate is met.

## Decision

Document understanding will use one strict native interpretation call with six
allow-listed tools: receipt, payslip, transaction, wealth observation, unknown
financial document, and reject. The gateway remains the only LLM boundary. The
model sees evidence and safe public context only; never household/database IDs.
The call returns typed observations, not financial decisions or mutation
capabilities. Go validates, reconciles, and owns every persistence transition.

The transitional `shadow` stage may make an independent interpretation call
beside legacy classification, persist no result, and compare only redacted
classification outcomes. `primary` cannot be enabled until all typed extraction
contracts, validation, targeted one-pass repair, review fallback, compatibility
checks, and monitoring are complete. Unknown configuration remains `legacy`.

This migration is intentionally additive. Bank Email remains fixed
`SPENDING_ONLY`; Financial Provider Email remains its existing generic
multi-observation pipeline. No provider-specific branches are introduced.

## Consequences

- The bounded tool set is fixed and strict; unknown tool names fail closed.
- Current production processing remains compatible while the new interpretation
  interface and safe rollout selector are introduced.
- No classifier output may mutate canonical state.
- Shadow metrics must exclude prompts, images, captions, tool arguments, and
  financial values.
- Primary rollout requires separate implementation and verification of
  field-level uncertainty, structured validation issues, targeted field-only
  repair (maximum one attempt), and review on any failed validation/repair.
