# SAVR Drift Guard Checklist

Use this checklist for every SAVR implementation PR.

A failed applicable item blocks completion.

## North star

- [ ] User provides a semantic fact/evidence once.
- [ ] Accepted facts are preserved downstream.
- [ ] Human interaction is limited to true **material** residual/policy/conflict.
- [ ] Correctness and auditability are not weakened.
- [ ] The attempted canonical outcome is named before deciding what may block it.
- [ ] Only uncertainty that can change that outcome, satisfy a hard canonical
      invariant, resolve a material evidence conflict, or obtain irreducible
      household policy may increase RHICE.
- [ ] Exact-but-non-material uncertainty is kept as provenance/quality metadata,
      not promoted into human work.

## Semantic ownership

- [ ] Every changed semantic dimension has one explicit current owner.
- [ ] No downstream layer silently reinterprets an accepted value.
- [ ] Any owner handoff is caused by unresolved state, user correction, new
      independent evidence, or canonical impossibility.
- [ ] Independent evidence verification is distinguished from redundant replay.

## Deterministic Go

- [ ] Go still owns authorization, canonical IDs, invariants, duplicate safety,
      compatibility, concurrency, and mutation.
- [ ] Go is not being used as a second NLP/semantic parser.
- [ ] Canonical validation targets the value/relationship it actually validates.
- [ ] Go does not use a generic confidence threshold as a third semantic vote
      after material facts are already accepted.
- [ ] Known canonical state supersedes a redundant missing source hint; an absent
      hint alone does not erase or reopen the known fact.
- [ ] A candidate query hit is not treated as canonical ambiguity until the
      source/domain contract establishes material plausibility.

## Known facts

- [ ] Validation failure does not erase unrelated accepted facts.
- [ ] ReviewDecision known facts include all safe accepted dimensions needed by
      the user/surface.
- [ ] No known fact is re-requested.

## Residual fidelity

- [ ] Missing/conflicting dimensions match the actual blocker.
- [ ] Every blocking residual is material to the attempted canonical outcome.
- [ ] Review type does not invent an unrelated residual.
- [ ] Quality signals, including generic confidence, are not mislabeled as
      semantic missingness.
- [ ] Human policy is explicit and not inferred.
- [ ] If the user cannot provide a meaningful fact/policy/choice, machine failure
      is not disguised as a human review.
- [ ] An IGNORE-only acknowledgement is justified as material user work; it is
      not used merely to dispose of machine uncertainty.

## Representation

- [ ] Genuine missingness is representable without sentinel values.
- [ ] Model/tool schema can represent the source state being handled.
- [ ] Representation repair cannot invent absent evidence.
- [ ] Canonical-required fields remain enforced at mutation time.
- [ ] A source-policy-derived canonical fact is allowed to satisfy the canonical
      requirement without pretending it was source-observed.
- [ ] Provenance does not, by itself, create review.
- [ ] Bank email without a printed transaction time keeps the intentional
      `EMAIL_RECEIVED_AT` canonical fallback and does not re-ask time.

## User authority

- [ ] Explicit confirmation/correction is not sent through redundant semantic
      approval.
- [ ] Hard canonical guards still apply.
- [ ] Concurrent/stale decisions still fail closed.

## Household knowledge

- [ ] Exact learned knowledge is reused before model/human escalation.
- [ ] Resolver is household-scoped, active, and unambiguous.
- [ ] No source-specific copy of semantic-learning SQL is added when a shared
      resolver exists/should exist.

## Domain continuity

- [ ] Review does not strip the source domain identity.
- [ ] Human-resolved path reaches the same domain finalizer as autonomous path.
- [ ] Finalizer parity is asserted on all material side effects.
- [ ] Generic transaction state is not mistaken for completion of a richer
      domain lifecycle.

## Intelligence routing

- [ ] PRD #144 call-order rules remain true.
- [ ] The path is minimum-sufficient: Go-only, Jev-only, LLM-only, or LLM→Jev
      only when the latter has a named residual/evidence purpose.
- [ ] No new "always Jev after LLM" behavior.
- [ ] A complete source-acceptable LLM result may proceed directly to Go
      canonical guards.
- [ ] No Jev semantic replay after explicit user confirmation.
- [ ] Additional Jev call has a named **material** bounded residual or independent
      evidence purpose.
- [ ] Provider failure is not treated as semantic approval or automatically as
      human uncertainty.
- [ ] Semantic sufficiency ends semantic review.

## UIR protection

- [ ] `review_item` / ReviewDecision remains canonical human-decision substrate.
- [ ] Web/Telegram resolution parity is not reimplemented.
- [ ] SAVR changes producer semantics, not channel-specific financial mutation.
- [ ] First-valid-write/stale behavior remains unchanged.

## YAGNI

- [ ] No generic workflow engine.
- [ ] No rules DSL.
- [ ] No semantic graph.
- [ ] No universal fact table unless an actual persistence gap is proven.
- [ ] No new AI layer/provider.
- [ ] No broad document-pipeline rewrite.
- [ ] No unrelated refactor.
- [ ] No schema migration unless current representation cannot encode the
      required invariant.

## Observability

- [ ] SAVR metrics reflect actual behavior.
- [ ] Intelligence telemetry writes succeed.
- [ ] Model-call order/count is tested for changed flows.
- [ ] Raw sensitive financial evidence is not added merely for metrics.

## Exit questions

Before marking a PR complete, answer:

1. What semantic fact was previously re-decided, erased, or misrepresented?
2. Who owns it now?
3. What canonical guard still protects the write?
4. What exact validation consequence remains?
5. What facts survive that consequence?
6. What does the human still need to provide, if anything?
7. Did model calls increase? If yes, what distinct material residual/evidence
   purpose justifies them?
8. For every human input: what canonical result can change because of the answer?
9. Could the same canonical outcome be reached safely without this review? If
   yes, why does the review exist?
10. Are any system-derived policy facts being demoted back to missing?
11. Are any canonical-known entities being overridden by absent redundant hints?
12. Do auto and human-resolved paths reach the same domain finalizer?
13. Did this PR accidentally absorb CEU or UIR scope?
14. Is there a new abstraction that can be deleted before merge?
