# UIR-SAVR Closure Drift Guard Checklist

A failed applicable item blocks closure.

## Scope

- [ ] Change closes UISC-01, UISC-02, UISC-03, or UISC-04 only.
- [ ] No CEU feature is pulled forward.
- [ ] No new finance product feature is introduced.
- [ ] No unrelated cleanup is bundled.

## Household-owned projection

- [ ] Email ingress household ownership remains canonical.
- [ ] BANK_EMAIL projection does not require Telegram source payload.
- [ ] FINANCIAL_EMAIL projection does not require Telegram source payload.
- [ ] `ProjectReviewItem` remains the single universal projection path.
- [ ] Recipient eligibility comes from household membership/Telegram identity.
- [ ] Originating chat is fallback only.
- [ ] Email address is not directly coupled to Telegram chat ID.
- [ ] Retry does not duplicate review/projection/recipient.
- [ ] First-valid-write/stale-action behavior is unchanged.

## SAVR observability

- [ ] Validator-induced review has an explicit measurable definition.
- [ ] Accepted dimensions at the validation boundary can be distinguished from
      true residuals.
- [ ] Semantic re-decision can be derived prospectively.
- [ ] Independent evidence verification is not falsely counted as re-decision.
- [ ] Residual Contract Fidelity is structural and deterministic.
- [ ] Known non-null facts cannot be counted as legitimate missing facts.
- [ ] Human supplied fields outside the residual are visible as a violation,
      except explicit correction.
- [ ] Historical missing provenance is unknown/incomplete, never coerced to zero.
- [ ] No raw prompt/evidence/financial value is persisted solely for metrics.
- [ ] No new model call is used to calculate product metrics.

## YAGNI

- [ ] Existing ReviewDecision/provenance is reused where sufficient.
- [ ] Existing intelligence phase telemetry is reused where sufficient.
- [ ] No new telemetry table unless existing storage demonstrably cannot express
      the required provenance safely.
- [ ] No semantic ontology/fact database is added.
- [ ] No generic rules/workflow/observability engine is added.

## Testing

- [ ] Integration fixtures use disposable test infrastructure.
- [ ] Primary projection acceptance models one household + one active user.
- [ ] Existing multi-recipient regressions stay green without becoming the
      closure's primary product assumption.
- [ ] No fake production transaction/email/household is created.
- [x] Rare unobserved source families retain corpus proof.

## Owner-as-canary

- [ ] Exact deployed SHA is recorded.
- [ ] Normal owner-household usage is the production observation cohort.
- [ ] Real correction/re-ask/unnecessary review is treated as a product finding.
- [ ] Kill-switch rollback remains available.
- [ ] No arbitrary tenant/event-volume quota is invented.
- [x] Product owner acceptance is recorded before full freeze.

## Freeze

- [x] S08-08 is closed only after household-owned email projection lands.
- [x] SAVR-09 is product-complete only after observability + production
      observation evidence.
- [x] SAVR-10 is full freeze only after SAVR-09 closure.
- [x] UIR + SAVR stop after this gate.
- [x] CEU starts only after merged/deployed closure acceptance.

## Final questions

1. Does any non-Telegram email source still need a Telegram chat in its payload
   merely to deliver a household review?
2. Can Operations distinguish a true residual from validation-created rework?
3. Can it identify semantic re-decision without reading prompts or raw values?
4. Can a review contract be checked for residual fidelity from stored state?
5. Are old uninstrumented rows reported honestly?
6. Did testing remain representative of the current single-household product?
7. Did production validation avoid synthetic financial state?
8. Is any new abstraction larger than the defect it solves?
