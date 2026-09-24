# ADR-045: Single-intelligence-pass routing with residual bounded rescue

## Status

Accepted architecture amendment — 2026-09-24.

Amends:

- ADR-038 System One semantic decision plane;
- ADR-041 Receipt auto-confirm;
- ADR-042 Screenshot row auto-confirm;
- ADR-040 Bank email zero-touch where merchant absence currently preempts category resolution.

Related product decision:

- docs/bdr/BDR-001-minimum-human-interaction-single-pass-intelligence.md
- docs/RICHMOD_INTELLIGENCE_ROUTING_MINIMAL_INTERACTION_PRD.md

## Context

ADR-038 correctly established Jev / System One as the preferred bounded semantic
decision plane and generative LLMs as the correct capability for arbitrary
extraction, vision, reasoning, and prose.

The implementation direction after the minimal-human-interaction PRD introduced
an ambiguity: "bounded semantic choice returns to Jev" can be interpreted as a
mandatory second semantic pass even after a generative call was required and
already returned a decisive constrained answer.

That creates a routine pattern:

~~~text
generative LLM
-> Jev repeats the same category/type decision
-> Go
~~~

The second model receives no independent evidence, owns the same fact, and often
exists only to agree with the first. It adds latency and an availability
dependency without reducing human work.

At the same time, removing Jev entirely after generative processing would be too
broad. Jev remains valuable when:

- a bounded fact is still unresolved;
- a distinct evidence-support predicate must be tested;
- a cheap bounded route can avoid the generative model entirely.

This ADR makes the routing rule explicit.

## Decision

Richmod adopts **one primary semantic owner per fact per event**.

The architecture chooses capability by question shape:

~~~text
deterministic exact state
-> Jev first when the task is already bounded and facts exist
-> generative only when arbitrary extraction/reasoning/vision is required
-> deterministic validation
-> Jev only for residual bounded uncertainty or an independently distinct
   evidence-verification claim
-> human only for irreducible uncertainty or policy intent
~~~

This is a decision tree, not a mandatory chain.

## Semantic ownership

For each canonical fact, the producing path MUST identify one semantic owner:

- DETERMINISTIC
- JEV
- GENERATIVE_EXTRACTION
- USER

Combined provenance such as GENERATIVE_PLUS_JEV is valid only when the two
passes own different stages, for example:

~~~text
generative extracts merchant/items/amount
Jev resolves only the still-undecided category
~~~

It is NOT valid merely because Jev repeated a category the generative model had
already decisively selected.

## Redundant semantic double-pass

A model pass is redundant when all are true:

1. an earlier intelligence pass already produced a policy-acceptable answer for
   semantic dimension D;
2. deterministic validation found no residual uncertainty on D;
3. the later pass asks substantially the same question about D;
4. the later pass is not testing an independent evidence-support predicate.

Redundant semantic double-pass is prohibited on the clear happy path.

## Jev-first rule

When Go already has all inputs required to form a bounded candidate set or
predicate, Jev SHOULD run before any generative call.

Examples:

- Telegram route;
- category among active household categories;
- review action among allowed actions;
- merchant-learning consent;
- salary-choice response;
- transfer purpose among server-owned choices;
- evidence-support yes/no questions.

If Jev can finish the event from existing facts, generative inference MUST be
skipped.

## Generative-first exception

Generative inference legitimately runs first when the required fact cannot be
enumerated or harvested without open-ended capability.

Examples:

- vision extraction;
- arbitrary merchant strings;
- free-form descriptions;
- multi-field document interpretation;
- complex conversational reasoning;
- arbitrary natural-language transformation into native-tool arguments.

After that call, Go performs source-specific deterministic validation.

If all required facts are complete and policy-acceptable, no Jev replay is
required.

## Residual bounded rescue

After generative inference, Go MUST identify the residual uncertainty set.

Example:

~~~text
known:
  amount
  date
  merchant
missing/undecided:
  category
~~~

Only category is eligible for residual Jev rescue.

The rescue request MUST NOT ask again for amount/date/merchant unless those
dimensions are independently unresolved.

If the rescue is decisive, Go may continue with zero human interaction.

If it remains undecided, ReviewDecision exposes only that residual fact.

## Independent evidence verification

A post-extraction bounded call is not redundant when it tests a different claim
against source evidence.

Example:

~~~text
Extraction:
  amount = 53,000

Independent evidence question:
  Does the original bank email actually support amount 53,000?
~~~

This is permitted because the second pass verifies evidence support rather than
re-extracting the amount.

Independent verification MUST be documented per source and MUST NOT be used as a
generic excuse to make every generative result pass through Jev.

## Source contracts

### Telegram simple transaction

Preferred call budget:

~~~text
0 generative
1 Jev bundle
~~~

when generic harvesting gives the required bounded candidates.

The bundle SHOULD include route and all transaction predicates that can share one
state snapshot.

### Telegram complex transaction / command

Preferred:

~~~text
1 Jev route
1 generative/native-tool pass if the route requires arbitrary capability
0 additional Jev on a clear validated result
~~~

Additional Jev is allowed only for named residual bounded dimensions.

A failure of the initial Jev-owned route MUST NOT silently delegate the same
route decision to generative mutation authority.

### Receipt

Preferred clear path:

~~~text
1 vision/generative extraction
0 Jev
~~~

provided source policy validates:

- required typed fields;
- printed/acceptable transaction date;
- amount;
- arithmetic where available;
- duplicate safety;
- category is constrained and policy-acceptable;
- no material conflict.

If category remains unresolved:

~~~text
1 vision
1 residual Jev category rescue
~~~

Do not ask Jev to repeat a category already accepted from the constrained vision
result.

### Screenshot

Preferred:

~~~text
1 vision extraction for the entire image
0 Jev for rows already decisive
<= 1 batched Jev request for unresolved bounded rows
~~~

The batch contains only unresolved rows/questions.

A clear row MUST NOT be included merely because another row needs rescue.

### Bank email

Deterministic parser/evidence state is preferred.

Jev may own:

- evidence-support predicates;
- new/merchant-less bounded category resolution.

Merchant absence MUST NOT automatically skip category resolution when enough
other evidence exists.

### Payslip

Generative document extraction may establish printed facts.

Go owns arithmetic/date/schema validation.

Primary-salary designation remains a user policy decision when unresolved.

A clear payslip MUST NOT receive a Jev call merely to approve the extraction if
no residual bounded semantic decision exists.

## Deterministic validation

"Skip the second model" does not mean "trust confidence".

Direct acceptance after a generative pass requires a source-specific acceptance
contract.

A contract SHOULD cover applicable items:

- schema/type validation;
- enum membership;
- amount format/range;
- date source/provenance;
- arithmetic;
- household authorization;
- account/entity exact resolution;
- duplicate/reconciliation constraints;
- source conflict detection;
- required-field completeness;
- explicit policy-choice exclusion.

Generative confidence may be one signal but MUST NOT be the sole gate.

## Missing evidence

If a required fact is absent from evidence, do not spend another model call to
guess it.

Example:

~~~text
receipt has amount + merchant + category
receipt has no transaction date
~~~

Required outcome:

~~~text
ReviewDecision.missing_facts = ["transaction_at"]
~~~

not:

~~~text
received_at becomes transaction time
~~~

and not:

~~~text
Jev guesses a date
~~~

## Failure semantics

### Jev-owned bounded question fails

Allowed:

- deterministic exact resolution;
- safe retry if failure is infrastructural and retry policy allows it;
- clarification;
- minimal review;
- safe stop.

Prohibited:

- silently asking a generative model the same bounded question and accepting it
  as if the Jev decision succeeded.

### Residual Jev rescue fails

Because a generative extraction already exists, the event may retain all valid
known facts and create a minimal review for only the unresolved residual fact.

The entire extraction MUST NOT be discarded.

### Generative extraction fails

Allowed:

- one documented repair attempt where ADR-037 allows it;
- minimal review from facts that remain valid;
- safe non-mutation.

Jev is not a replacement for arbitrary values the generative extractor failed to
produce.

## Batching

When multiple bounded residual questions share the same event and candidate
space, they SHOULD be batched into one Jev call.

For screenshots, unresolved rows SHOULD share one request when practical.

Batching MUST NOT cause already-resolved rows to be re-judged.

## ReviewDecision integration

Before user interaction, every path MUST compute:

- known facts;
- proposed facts;
- missing facts;
- conflicting facts;
- bounded residual choices;
- human policy choices.

Only unresolved entries may appear as required controls.

A model call that cannot reduce this residual set is presumptively unnecessary.

## Telemetry

Record enough provenance to derive:

- Jev calls per canonical event;
- generative calls per canonical event;
- total intelligence passes per canonical event;
- residual rescue count;
- residual rescue success;
- redundant semantic double-pass;
- model-phase latency;
- decision source by source type.

A combined decision source MUST identify which fact Jev actually resolved.

Do not report GENERATIVE_PLUS_JEV simply because both models were called.

## Regression requirements

Every implementation changing model ordering MUST test:

1. a clear bounded event does not call generative;
2. a clear generative extraction does not call redundant Jev;
3. one residual bounded uncertainty calls Jev before human review;
4. missing evidence does not trigger model guessing;
5. Jev failure on a Jev-owned decision does not silently become generative
   mutation authority;
6. clear and unresolved items in one batch take different paths;
7. provenance reports only the model phases that actually ran.

## Consequences

Positive:

- lower p50/p95 latency on clear AI-assisted events;
- fewer provider dependencies on happy paths;
- lower inference cost;
- unchanged minimal-human-interaction objective;
- clearer ownership of semantic decisions;
- Jev remains valuable precisely where bounded judgment is useful.

Trade-offs:

- source-specific acceptance contracts become more important;
- telemetry must distinguish residual rescue from redundant replay;
- tests must assert model-call count/ordering in addition to canonical outcomes.

## Supersession

Where this ADR conflicts with older language:

- ADR-038's "return to Jev" means only **remaining unresolved bounded semantic
  choice**, not every bounded field produced by a generative extractor;
- ADR-041 is amended so a clear receipt does not require Jev after a decisive
  constrained extraction;
- ADR-042 is amended so only unresolved screenshot rows enter the bounded batch;
- ADR-040 is amended so merchant absence does not by itself prevent bounded
  category resolution.

ADR-038 remains authoritative for gateway, failure, canonical authority, and
bounded candidate ownership. This ADR changes routing/order, not the LiteRouter
boundary or PostgreSQL/Go authority.
