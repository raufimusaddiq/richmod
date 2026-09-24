# BDR-001: Minimum Human Interaction with Single-Pass Intelligence

## Status

Accepted product decision — 2026-09-24.

## Decision owner

Product Design.

## Related documents

- `docs/RICHMOD_INTELLIGENCE_ROUTING_MINIMAL_INTERACTION_PRD.md`
- `docs/RICHMOD_MINIMAL_HUMAN_INTERACTION_VALID_DATA_PRD.md`
- `docs/adr/ADR-038-system-one-semantic-decision-plane.md`
- `docs/adr/ADR-045-single-intelligence-pass-routing.md`
- `docs/plans/intelligence-routing-minimal-interaction-execution.md`

## Business problem

Richmod exists to remove bookkeeping work from a household. The product is not
successful merely because it uses AI, Jev, or sophisticated review flows. It is
successful when a valid financial event reaches canonical state with the least
necessary human effort.

The first minimal-interaction PRD correctly established this product north star,
but implementation guidance could be read as a linear intelligence pipeline:

```text
deterministic
-> Jev
-> generative
-> Jev
-> Go
-> human
```

That interpretation creates a new kind of product friction: the user may no
longer be asked unnecessary questions, but every event pays unnecessary model
latency and cost. In particular, asking Jev to repeat a semantic decision that a
required generative extractor already made decisively does not create new
evidence.

The opposite simplification is also wrong:

```text
LLM says confidence=0.97
-> trust it
-> canonical write
```

Model self-confidence is not financial evidence, and minimizing latency does not
justify weakening canonical correctness.

This BDR chooses the product trade-off explicitly so future implementation does
not oscillate between those two extremes.

## Product decision

Richmod optimizes lexicographically:

1. **Financial correctness and auditability are hard constraints.**
2. **Within those constraints, minimize required human interaction.**
3. **Among equally correct, equally low-human-interaction paths, minimize
   intelligence passes, latency, and cost.**

The primary human metric remains:

```text
Required Human Inputs per Canonical Financial Event (RHICE)
```

The product therefore prefers one cheap machine decision over one avoidable
human decision, but it does not prefer an unnecessary second machine decision
when the first intelligence owner already produced a complete, valid result.

## Core principle

> Use the cheapest sufficient intelligence once. Escalate only for residual
> uncertainty. Ask a human only for irreducible uncertainty or policy intent.

"Cheapest" here does not mean monetary cost only. It includes latency,
availability dependencies, implementation complexity, and user-perceived delay.

## Semantic ownership

Every semantic fact has one primary owner for a given turn/event.

Examples:

- deterministic merchant memory may own category;
- Jev may own a bounded category choice;
- a vision LLM may own extraction of a printed merchant string;
- Go owns arithmetic, authorization, duplicate constraints, ID mapping, and
  canonical mutation;
- the user owns household policy choices.

A second model MUST NOT re-decide the same semantic fact merely because it is
available.

A second model is justified only when it does one of these:

1. resolves a still-unresolved bounded fact;
2. verifies a materially different claim against independent source evidence;
3. provides a capability the first model cannot provide.

This distinction is the business definition of **residual intelligence**.

## Accepted decision tree

```text
source evidence / user intent
        |
        v
deterministic exact state / validation
        |
        +------------------------------+
        |                              |
 bounded problem with facts?     arbitrary extraction,
        |                        vision, reasoning, prose?
       yes                             yes
        |                               |
        v                               v
       Jev                         generative LLM
        |                               |
 decisive?                        Go validation
   |       |                         |
  yes      no                  complete + safe?
   |       |                    |          |
   v       |                   yes         no
   Go      |                    |           |
           |                    v           |
           |                   Go      bounded residual?
           |                                |
           |                           yes  |  no
           |                            |    |
           +---- generative if          v    v
                 arbitrary value       Jev  human / safe stop
                 is actually needed     |
                                   decisive?
                                    |    |
                                   yes   no
                                    |     |
                                    v     v
                                    Go   human
```

This diagram is a decision tree. It is explicitly NOT a sequence that every
event must execute.

## Human interaction decision

Human interaction is required only for:

- evidence that is genuinely absent;
- evidence conflict that deterministic policy cannot resolve;
- duplicate/reconciliation ambiguity;
- a bounded semantic question that machine intelligence remains undecided on;
- household policy or intent;
- deliberate correction/approval where product policy requires it.

If a machine path has sufficient information to decide safely, asking the user
for confirmation "just in case" is product debt.

## Intelligence interaction decision

The following is also product debt:

```text
LLM decisive on category
-> Jev asked the same category question
-> same answer
```

The second call is a **redundant semantic double-pass** unless it tests a distinct
evidence claim.

The product target for redundant semantic double-pass rate is approximately zero.

## Accepted source strategies

### Simple Telegram

Preferred:

```text
Go harvest
-> one Jev bundle
-> Go
```

Jev may decide route plus all bounded transaction predicates in the same request.
Do not invoke a generative agent when the bounded bundle is sufficient.

### Complex Telegram

Preferred:

```text
Jev route
-> generative/native-tool reasoning because arbitrary capability is needed
-> deterministic validation
-> Go
```

Do not automatically run a second full transaction-semantic Jev call.

A post-generative Jev call is justified only for a specific residual bounded fact
or distinct evidence-support question that remains after validation.

### Receipt image

Preferred clear path:

```text
vision LLM
-> deterministic schema/date/arithmetic/duplicate/category validation
-> Go
```

If category remains genuinely unresolved:

```text
vision LLM
-> Go identifies residual category
-> one bounded Jev rescue
-> Go or minimal review
```

Do not make Jev repeat a decisive constrained vision category.

### Transaction screenshot

Preferred:

```text
one vision extraction for image
-> per-row deterministic validation
-> clear rows proceed
-> unresolved bounded rows collected
-> one batched Jev rescue
-> Go / minimal review
```

Do not send already-decisive rows through Jev, and do not make one Jev request
per row when the unresolved questions can be batched safely.

### Bank email

Structured evidence should prefer deterministic parsing + bounded judgment.

Independent evidence verification is allowed because it answers a different
question from extraction:

```text
"Is 53,000 the extracted amount?"     extraction/classification
"Does this email support 53,000?"     independent evidence claim
```

These are not redundant semantic ownership.

Merchant absence is not itself a reason to ask the human if category can still
be decided from available evidence.

### Payslip

Vision/extraction may establish salary facts. Go validates arithmetic/date/schema.

Whether a source becomes the household's primary salary is a human policy choice
when not already established. Jev/LLM must not infer that policy merely to drive
RHICE to zero.

## Alternatives considered

### Alternative A — Always Jev after generative

Rejected.

Benefits:

- superficially consistent bounded authority;
- extra probability distribution for every bounded field.

Costs:

- redundant latency on clear paths;
- additional provider dependency;
- additional failure point;
- higher cost;
- duplicated semantic ownership;
- no new evidence when Jev simply reclassifies the generative result.

This is acceptable only when Jev resolves residual uncertainty or independently
tests source support.

### Alternative B — Generative-only whenever confidence is high

Rejected.

Benefits:

- low call count;
- simple implementation.

Costs:

- self-reported confidence becomes de facto financial authority;
- no clear distinction between observed, inferred, and missing facts;
- easier hallucination-to-ledger path;
- inconsistent with fail-closed financial semantics.

Generative output may be accepted directly only as part of a source-specific
contract with constrained schema and deterministic validation. Confidence alone
is never sufficient.

### Alternative C — Human confirmation for every AI-assisted event

Rejected.

Benefits:

- superficially conservative.

Costs:

- defeats the product;
- RHICE stays high;
- users re-enter/approve facts the system already knows;
- "AI-assisted data entry" replaces autonomous bookkeeping.

### Alternative D — Deterministic + capability-directed intelligence + residual rescue

Accepted.

It preserves correctness while minimizing both human and machine waste.

## Business metrics

### Primary

**RHICE — Required Human Inputs per Canonical Financial Event**

Target:

- clear event: 0;
- one irreducible bounded uncertainty: <= 1;
- N irreducible missing facts: <= N.

### Correctness guardrail

**Post-Auto-Confirm Material Correction Rate**

Must not materially regress while RHICE improves.

### Intelligence efficiency

Track:

- generative calls / canonical event;
- Jev calls / canonical event;
- total intelligence passes / canonical event;
- redundant semantic double-pass rate;
- residual Jev rescue success rate;
- p50/p95 intelligence latency by source;
- time to canonical state.

### Diagnostic interpretation

High residual rescue success + low correction rate:
Jev is eliminating human work where it adds value.

High double-pass rate + low disagreement:
the second model call is likely waste.

High accepted-without-edit review rate:
the product is still over-reviewing.

Low model calls + rising correction rate:
the system is cutting intelligence too aggressively.

## Revisit triggers

Revisit this decision if:

- post-auto-confirm correction materially increases;
- a source-specific independent verifier demonstrably catches errors that the
  single-pass path cannot catch;
- LiteRouter/Jev latency changes enough to alter user-perceived trade-offs;
- a new model capability reliably returns externally verifiable evidence spans
  that change the validation model;
- RHICE decreases but time-to-canonical-state materially worsens from model
  chaining.

Any revisit must compare against this BDR's lexicographic product priorities.
It must not optimize model elegance at the expense of user attention.

## Product invariant

The target product is:

```text
minimum necessary human interaction
+
minimum necessary intelligence work
+
deterministic canonical authority
```

not:

```text
maximum model consensus
```
