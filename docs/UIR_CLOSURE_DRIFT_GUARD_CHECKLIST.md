# UIR Closure Drift Guard Checklist

Use this checklist for every UIRC implementation PR.

A failed applicable item blocks completion.

## Product boundary

- [ ] The change closes an existing UIR contract gap.
- [ ] It does not add a new finance feature.
- [ ] It does not absorb SAVR semantic-authority work.
- [ ] It does not absorb CEU work.
- [ ] It does not expand Wealth scope beyond the current review blocker.

## YAGNI

- [ ] Existing `review_item` / `ReviewDecision` / `review_request` are reused.
- [ ] Existing jobs/Telegram transaction flow are reused where applicable.
- [ ] No new workflow engine/framework is introduced.
- [ ] No new model call is added for a deterministic bounded action.
- [ ] No schema change is introduced unless current storage cannot represent the
      required invariant safely.
- [ ] No unrelated cleanup/refactor is bundled.

## Web escape

- [ ] Ordinary blocker is completable from Telegram.
- [ ] Optional View details is not confused with required continuation.
- [ ] Voluntary Wealth richer-workflow navigation is not counted as mandatory
      escape.
- [ ] Expired projection does not force Web while canonical item remains open.

## Canonical resolver

- [ ] Web and Telegram use the same financial transition rules.
- [ ] Surface adapters contain transport/presentation logic, not duplicated
      canonical mutation.
- [ ] Household/candidate/current-state validation remains server-side.
- [ ] First valid resolution wins.
- [ ] Stale second action cannot mutate again.

## Canonical Inbox

- [ ] Current review work is represented by `review_item`.
- [ ] ReviewDecision remains the unresolved-fact contract.
- [ ] Any legacy fallback is explicitly compatibility-only.
- [ ] A new current producer cannot bypass canonical item creation.

## Metrics

- [ ] Resolution surface comes from actual resolution, not user identity.
- [ ] Web Escape Rate counts required Web escape only.
- [ ] TARC requires completion capability, not delivery only.
- [ ] Admin metrics remain read-only and do not expose raw financial evidence.
- [ ] Metrics can be verified without manual SQL.

## Producer coverage

- [ ] Active producer/capability source of truth is used by production code.
- [ ] New unregistered producer/type fails CI.
- [ ] Compatibility-only review types are explicit.
- [ ] A delivered-but-dead-end fixture fails actionability coverage.

## SAVR protection

This PR MUST NOT redesign:

- [ ] payslip arithmetic/period representation;
- [ ] extraction schemas;
- [ ] merchant_alias reuse;
- [ ] natural-language semantic ownership;
- [ ] LLM/Jev accepted-fact preservation;
- [ ] validator consequence taxonomy;
- [ ] known-fact re-ask behavior;
- [ ] unified document interpretation.

If any checked item above becomes necessary to finish the task, stop and raise a
product-design question instead of silently expanding scope.

## Final questions

Before marking a UIRC task complete:

1. What exact UIR contract violation did this change close?
2. Could the same result have been achieved by reusing an existing domain path?
3. Does any ordinary Telegram blocker still say or encode `requires_web=true`?
4. Do Web and Telegram reach the same canonical mutation?
5. Can a Telegram-linked user resolving on Web be measured as WEB?
6. Does a delivered but non-completable review stay out of TARC?
7. Can a new producer silently bypass the capability gate?
8. Did this PR touch any SAVR-owned semantic behavior?
9. Is there any new abstraction we can delete before merge?

If an answer is unclear, the task is not complete.
