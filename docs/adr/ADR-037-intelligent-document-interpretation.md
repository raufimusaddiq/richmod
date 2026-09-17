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

Extraction validation reports deterministic `{field, code}` issues. When any
issue exists, Go may run exactly one field-restricted repair call: the model
receives only the flagged field names plus issue codes, may return only those
fields, and Go revalidates with the same validator before accepting the patch.
A failed, malformed, out-of-allowlist, or absent repair keeps the original
invalid result and sends the document to Review. Repair never mutates state.

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

## Current rollout state

The legacy classify-then-extract pipeline remains the production path.

- All six bounded interpretation tools decode typed per-family observations
  with per-field `{value, status: PRESENT|MISSING|AMBIGUOUS, confidence}`.
  Unknown tools, malformed JSON, out-of-family payloads, and invalid statuses
  fail closed into Review; legacy/unknown mode behavior is unchanged.
- `RICHMOD_DOCUMENT_INTERPRETATION=shadow` records one redacted
  `INTERPRETATION_SHADOW` classification row beside the legacy classification
  plus an `INTERPRETATION_SHADOW_METRIC` row containing only class agreement,
  bounded agree/disagree/malformed counters per document type, a bounded error
  class, and latency; prompts, evidence, captions, filenames, tool arguments,
  amounts, merchants, and identifiers are never persisted. Shadow never
  mutates canonical state.
- Field-restricted repair is wired into the production receipt, payslip, and
  screenshot validators with same-validator revalidation. A failed, malformed,
  or out-of-allowlist repair appends the deterministic `REPAIR_FAILED` issue to
  the original validation issues and keeps the invalid document on Review.
- The typed dispatch contract includes `document_type_confidence` (0..1),
  `quality` (`CLEAR|DEGRADED|UNREADABLE`), complete per-field status/value
  observations, critical-only `field_confidence`, and bounded
  `missing_fields`/`ambiguous_fields`. Go rejects out-of-range values, unknown
  enum members, extra fields, inconsistent lists, and non-critical confidence
  keys. The review floor is 0.80; any lower document/critical-field confidence,
  non-clear quality, missing field, or ambiguous field routes to Review.
- Typed interpretation remains observations only. It cannot provide canonical
  IDs or mutate canonical state. The existing per-document extractors remain
  the canonical candidate source until their field schemas/validators consume
  the typed dispatch directly; do not enable `primary` ahead of that migration.
