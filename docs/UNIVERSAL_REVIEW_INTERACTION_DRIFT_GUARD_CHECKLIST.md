# Universal Review Interaction Drift Guard Checklist

Use for every UIR task/PR.

Source contract:

- docs/RICHMOD_UNIVERSAL_REVIEW_INTERACTION_PRD.md
- docs/bdr/BDR-002-review-inbox-channel-independent.md
- docs/adr/ADR-046-universal-review-interaction-projection.md

A failed applicable item blocks completion.

---

## A. Canonical review domain

- [ ] review_item remains canonical.
- [ ] Telegram delivery failure cannot remove canonical review.
- [ ] Web availability does not depend on Telegram.
- [ ] resolution locks/revalidates current review state.
- [ ] first valid resolution wins.
- [ ] stale actions cannot mutate canonical state twice.

## B. Shared resolver

- [ ] Web and Telegram call the same canonical domain operation.
- [ ] Telegram does not duplicate financial mutation SQL/rules.
- [ ] allowed_actions comes from canonical review policy.
- [ ] missing_facts is enforced server-side.
- [ ] household authorization is revalidated.
- [ ] category/account/Wealth/candidate membership is revalidated.
- [ ] audit is emitted once.
- [ ] RHICE telemetry counts actual human-supplied fields/actions.

## C. Telegram projection

- [ ] eligible review has one idempotent projection.
- [ ] projection points to review_item directly.
- [ ] projection does not require a transaction for non-transaction reviews.
- [ ] all eligible recipients are authorized active household members.
- [ ] send failure is retryable/observable.
- [ ] queued send rechecks review status.
- [ ] projection expiration does not silently discard canonical review.

## D. ReviewDecision parity

- [ ] Telegram known facts match canonical decision.
- [ ] Telegram missing facts match canonical decision.
- [ ] Telegram allowed actions match canonical decision.
- [ ] Telegram does not infer category/date/etc. from review_type against decision.
- [ ] partial resolution updates the stored residual state.
- [ ] Web and Telegram show the same remaining blocker after partial resolution.

## E. Actionability

- [ ] ordinary blocker is completable in Telegram.
- [ ] no primary "Open Web to continue" outcome remains for covered type.
- [ ] View details is optional.
- [ ] category is actionable.
- [ ] date is actionable.
- [ ] compound residual is actionable.
- [ ] duplicate/conflict is actionable.
- [ ] transfer classification is actionable.
- [ ] salary policy/date is actionable.
- [ ] financial-email entity resolution is actionable.
- [ ] Wealth review is actionable.
- [ ] cycle residual review is actionable.

## F. Conversation safety

- [ ] callback/reply exact binding preserved.
- [ ] open review does not capture unrelated chat.
- [ ] multiple open reviews do not shrink general chat capability.
- [ ] stale reply is handled deterministically.
- [ ] normal Telegram conversational behavior from PRD #144 remains.

## G. Candidate safety

- [ ] callbacks never trust stale canonical IDs blindly.
- [ ] candidates are household-scoped and active.
- [ ] duplicate candidate is revalidated.
- [ ] Wealth Account compatibility is revalidated.
- [ ] account relationship validation remains deterministic.
- [ ] model never chooses hidden canonical IDs.

## H. Intelligence routing

- [ ] deterministic callback needs no new AI call.
- [ ] bound free-form interpretation uses only necessary intelligence.
- [ ] no new LLM->Jev replay was introduced.
- [ ] PRD #144 source acceptance/failure semantics remain unchanged.
- [ ] human policy remains human-owned.

## I. Cross-surface sync

- [ ] Telegram resolution disappears/resolves in Web.
- [ ] Web resolution disables Telegram actions.
- [ ] multi-recipient first-write wins.
- [ ] stale callback returns already resolved, not error-prone mutation.
- [ ] failed message edit does not roll back canonical resolution.

## J. Review-type coverage

- [ ] schema review types enumerated.
- [ ] producible review types identified.
- [ ] legacy-only types documented.
- [ ] every producible type maps to FULL Telegram capability.
- [ ] adding a new producible type without a renderer/capability test fails.

## K. Metrics and Admin observability

- [ ] actionable review coverage measurable.
- [ ] Web escape rate measurable.
- [ ] completion surface measurable.
- [ ] stale action attempts measurable.
- [ ] delivery health measurable.
- [ ] resolution latency p50/p95 measurable.
- [ ] every rollout/DoD metric has a deterministic Admin aggregate API.
- [ ] `/admin?tab=reviews` renders headline rollout health.
- [ ] Admin exposes per-review-type coverage so global averages cannot hide drift.
- [ ] Admin exposes safe projection/delivery diagnostics.
- [ ] Admin Overview exposes compact review health without duplicating detail.
- [ ] household Admin detail can diagnose Telegram review delivery/actionability.
- [ ] no rollout metric requires direct PostgreSQL inspection.
- [ ] no raw financial evidence is stored or exposed just for metrics.
- [ ] Admin telemetry is read-only and cannot resolve canonical reviews.
- [ ] RHICE semantics unchanged.

## L. Scope

- [ ] no unrelated dashboard redesign.
- [ ] no change to canonical financial authority.
- [ ] no threshold tuning.
- [ ] no provider-specific AI branch added.
- [ ] no Web removal.
- [ ] no command-only regression in Telegram.

---

## Final gate

Before completion answer:

1. Can this review be finished in Telegram without opening Web?
2. Does Web reach the exact same canonical resolver?
3. What happens if Web resolves one millisecond before Telegram?
4. What happens if two Telegram recipients act?
5. What exact ReviewDecision residual is being collected?
6. Does this add any unnecessary model call?
7. Which test prevents this review type from becoming Web-only again?
8. Can an operator verify the relevant rollout metric in Admin without SQL?
9. Does the Admin surface expose only safe operational metadata?

If any answer is unclear, the task is not complete.
