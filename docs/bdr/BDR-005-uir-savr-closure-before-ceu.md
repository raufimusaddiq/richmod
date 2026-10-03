# BDR-005 — Close UIR + SAVR on the Real Owner Household Before CEU

**Status:** ACCEPTED PRODUCT DECISION  
**Date:** 2026-09-28  
**Baseline:** `main@f6b2d374fe7c45bdb8d69507c39596f6945e906a`  
**Decision owner:** Product Design

## Context

UIR and SAVR are functionally delivered, but the final combined audit found two
bounded closure issues:

- email-origin reviews can remain Inbox-only because bank/financial-email
  producers pre-gate Telegram projection on originating Telegram payload, even
  though canonical household ownership and universal household-recipient
  resolution already exist;
- SAVR is deployed and corpus-covered, but three north-star metrics are not yet
  measurable and its current live-canary wording assumes a disposable household
  that Richmod does not have.

Richmod is currently a personal production system with one real household. The
product owner is also the primary real user.

## Decision

Run one **UIR-SAVR Closure Sprint** before CEU.

The sprint will:

1. make review delivery household-owned rather than source-provenance-owned;
2. complete the minimum telemetry needed to measure validator-induced human
   review, residual contract fidelity, and semantic re-decision;
3. use the real owner household as the production canary through ordinary usage;
4. keep synthetic data in disposable test infrastructure, not production;
5. freeze UIR + SAVR after the combined gate passes.

## Recipient authority

Source provenance answers where evidence came from.

Household ownership answers whose financial state and review it belongs to.

Review recipient policy answers which active household identity receives the
projection.

These concerns must not be collapsed.

For email-origin reviews:

```text
email_ingress_address.household_id
-> source_event.household_id
-> review_item / review_request household
-> household_member + telegram_identity
-> review_request_recipient
```

Originating Telegram chat is a fallback, not a prerequisite.

## Production validation decision

The current product does not need a second household to prove its own rollout.

Production validation uses:

- normal real owner-household usage;
- real review/correction telemetry;
- existing kill switches;
- explicit product-owner acceptance;
- integration/corpus tests for edge cases not naturally observed.

No fake production transaction is required.

A rare source family may be marked `PRODUCTION_UNOBSERVED` when its
deterministic/integration corpus is green. Absence of artificial traffic is not
a product failure.

## Observability decision

The old `Residual Fidelity Rate` ground-truth framing is superseded for this
product stage by **Residual Contract Fidelity**: a deterministic check that the
stored review contract asks only for genuinely unresolved dimensions and does
not require undeclared semantic inputs to finish.

Historical rows lacking new provenance are unknown, not zero.

## Rejected alternatives

### Create a disposable production household

Rejected. It adds fake product state solely to satisfy a rollout ritual and does
not reflect current real usage.

### Seed fake transactions/emails into the owner household

Rejected. It contaminates canonical financial state.

### Keep the three metrics permanently not measurable

Rejected. They express core SAVR promises and can be made measurable with bounded
provenance instrumentation.

### Add a semantic fact platform

Rejected as YAGNI. Existing ReviewDecision and intelligence telemetry are nearly
sufficient.

### Bind each email address directly to a Telegram chat

Rejected. Email ownership is household-scoped; recipient selection belongs to
the review projection layer.

## Exit

CEU may start only after the UIR-SAVR Closure PRD Definition of Done is accepted
on merged/deployed main.

After that point, UIR and SAVR are frozen except for production defects against
their approved contracts.

## Closure record

**UISC-04, 2026-10-03:** the product owner accepted the owner-household
observation. Email-origin projection and the Jago/Bibit rechecks were not
naturally observed and remain `PRODUCTION_UNOBSERVED` with green corpus evidence.

```text
UIR frozen
SAVR frozen
owner-household production observation accepted
CEU may start
```
