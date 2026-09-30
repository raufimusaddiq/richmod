# Intelligence Routing Drift Guard Checklist

Use this checklist before marking any /goals task or implementation PR complete.
Record per-task results in `docs/plans/intelligence-routing-task-status.md`;
the ledger does not waive any gate below.

Source contract:

- docs/RICHMOD_INTELLIGENCE_ROUTING_MINIMAL_INTERACTION_PRD.md
- docs/bdr/BDR-001-minimum-human-interaction-single-pass-intelligence.md
- docs/adr/ADR-045-single-intelligence-pass-routing.md

A failed item blocks completion unless the PR explicitly documents why the item
does not apply.

---

## A. Product outcome

- [ ] Does the change reduce or preserve RHICE?
- [ ] Does it avoid asking for any fact Richmod already knows?
- [ ] If the user is asked something, is that exact fact truly unresolved?
- [ ] If the user supplies N fields/choices, does telemetry count N inputs?
- [ ] Does correctness remain a hard constraint?
- [ ] Is post-auto-confirm correction risk unchanged or explicitly guarded?

---

## B. Intelligence routing

- [ ] If the task was already bounded and facts existed, was Jev considered
      before generative inference?
- [ ] If Jev resolved the bounded task, is the generative call skipped?
- [ ] If generative inference was genuinely required, does Go validate the
      result before canonical mutation?
- [ ] If the generative result is complete and policy-acceptable, is redundant
      Jev replay skipped?
- [ ] If a bounded fact remains unresolved, does Jev rescue only that residual
      fact?
- [ ] Is any post-generative Jev call asking a distinct question rather than
      repeating the same semantic dimension?
- [ ] Can multiple residual bounded questions share one safe batched request?
- [ ] Are already-resolved items excluded from residual batch calls?

---

## C. Semantic ownership

For every canonical fact touched by the change:

- [ ] Is there one primary semantic owner?
- [ ] Is the owner one of DETERMINISTIC / JEV / GENERATIVE_EXTRACTION / USER?
- [ ] If provenance says GENERATIVE_PLUS_JEV, did the two models actually own
      different stages/facts?
- [ ] Is Jev prevented from choosing hidden canonical DB identifiers?
- [ ] Does Go still map semantic values to canonical IDs?

Red flag:

~~~text
LLM decides X
-> X already passes source policy
-> Jev decides X again
~~~

If this occurs, explain the independent correctness claim. Otherwise it is drift.

---

## D. Generative acceptance

- [ ] Is confidence NOT the sole acceptance gate?
- [ ] Are schema/types validated?
- [ ] Are enum values server-owned/validated where applicable?
- [ ] Is IDR amount valid?
- [ ] Is date provenance explicit?
- [ ] Are arithmetic constraints checked where applicable?
- [ ] Is duplicate/reconciliation safety checked?
- [ ] Is household authorization checked?
- [ ] Are required fields complete?
- [ ] Are explicit ambiguity/missing statuses respected?
- [ ] Are human-policy choices excluded from automatic inference?

---

## E. Missing evidence

- [ ] Does absent evidence remain absent rather than guessed?
- [ ] Is received/upload/processing time prevented from masquerading as an
      observed transaction time?
- [ ] Does merchant remain NULL when source evidence has no merchant and merchant
      is optional?
- [ ] Does a missing fact become ReviewDecision.missing_facts?
- [ ] Does the UI ask only for that missing fact?
- [ ] Does Telegram ask for the same residual fact as Web?

---

## F. ReviewDecision

- [ ] known_facts contains only actually known/supported facts.
- [ ] proposed_facts contains the current proposal.
- [ ] missing_facts exactly names unresolved required facts.
- [ ] conflicting_facts preserves real evidence conflicts.
- [ ] bounded_choices contains only server-owned choices.
- [ ] why_not_auto_confirm explains the actual blocker.
- [ ] backend refuses confirmation while canonical-required missing facts remain.
- [ ] legacy clients cannot bypass the residual guard.
- [ ] optional fields do not become required because a review exists.

---

## G. Jev failure semantics

- [ ] Infrastructure failure is distinct from semantic negative/undecided.
- [ ] Initial Jev-owned mutation decision failure does not silently fall back to
      generative authority for the same question.
- [ ] Residual Jev failure preserves valid generative facts.
- [ ] Residual Jev failure produces minimal review, not full re-entry.
- [ ] Retries do not turn multiple probabilistic draws into an unsafe
      "any-positive-wins" policy.

---

## H. Source-specific gates

### Telegram

- [ ] Simple harvestable transaction uses no generative call.
- [ ] Complex clear transaction has no full post-LLM semantic replay.
- [ ] Exact bindings still outrank generic route.
- [ ] Implicit bindings remain route-gated.
- [ ] One open review cannot swallow a new event.

### Receipt

- [ ] Clear receipt can finish with 1 vision call, 0 Jev.
- [ ] Jev runs only for residual bounded uncertainty.
- [ ] Missing date does not trigger model guessing.
- [ ] Duplicate ambiguity remains fail-closed.

### Screenshot

- [ ] Clear rows are excluded from Jev.
- [ ] Unresolved rows are batched where safe.
- [ ] One uncertain row does not force all rows through Jev.
- [ ] Provenance on clear rows does not claim Jev.
- [ ] Missing date and category conflict can coexist in residual facts.

### Bank Email

- [ ] Learned merchant rule remains deterministic-first.
- [ ] Merchant-less expense can still attempt bounded category resolution.
- [ ] Merchant is never fabricated.
- [ ] Existing evidence-verification semantics remain intact.

### Payslip

- [ ] Evidence facts and salary-policy choice are separate.
- [ ] Existing primary salary state prevents redundant classification prompt.
- [ ] Missing date alone asks date alone.

---

## I. Telemetry

- [ ] RHICE counts actual user-supplied controls/fields.
- [ ] Server-known merged values are not counted as human input.
- [ ] Jev and generative phases are separately attributable.
- [ ] Phase purpose is recorded.
- [ ] Residual rescue can be measured.
- [ ] Redundant semantic double-pass can be measured.
- [ ] Model latency can be measured without storing sensitive raw evidence.
- [ ] Combined provenance is emitted only when both phases materially contributed.

---

## J. Tests

- [ ] Tests assert final canonical state.
- [ ] Tests also assert model-call count/order where relevant.
- [ ] A clear bounded path proves generative was not called.
- [ ] A clear generative path proves redundant Jev was not called.
- [ ] A residual path proves Jev ran before human review.
- [ ] A missing-evidence path proves no model guessed the fact.
- [ ] Provider-failure tests remain fail-closed.
- [ ] Test expectations follow the current PRD, not legacy implementation.
- [ ] No test was weakened merely to make the implementation green.

---

## K. Scope

- [ ] No unrelated refactor.
- [ ] No unrelated UI redesign.
- [ ] No threshold tuning bundled without explicit product evidence.
- [ ] No new provider-specific branch unless separately approved.
- [ ] No direct TypeSafe/provider call outside LiteRouter.
- [ ] No change to canonical authority.

---

## Final gate

Before completion, answer:

1. What human interaction did this change remove or preserve?
2. What intelligence call did this change remove, add, or preserve?
3. Why is every remaining human interaction necessary?
4. Why is every remaining model interaction necessary?
5. What exact residual uncertainty remains when auto-confirm is blocked?
6. What test prevents the old drift from returning?

If any answer is vague, the task is not ready.
