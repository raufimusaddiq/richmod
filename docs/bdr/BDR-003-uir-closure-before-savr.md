# BDR-003 — Close UIR Contract Drift Before SAVR

**Status:** ACCEPTED PRODUCT DECISION  
**Date:** 2026-09-27  
**Baseline:** `main@fabe7368c76dff2032685e23ef037bcefe36dcef`

## Context

UIR-00..UIR-10 delivered the intended review foundation:

- canonical `review_item`;
- ReviewDecision-driven rendering;
- universal Telegram projection;
- shared review-domain operations for major families;
- cross-surface locking;
- Admin Review Ops.

A post-delivery product audit found a bounded set of gaps:

- a few Telegram actions still require Web;
- transfer reconciliation still has duplicated canonical mutation;
- some financial-email lifecycle work remains surface-owned;
- Review Inbox reads still retain a legacy transaction-first path;
- completion-surface / Web-escape / TARC semantics can overstate UIR success;
- producer coverage is asserted from a manual list rather than structurally
  enforced.

SAVR is the next architectural sprint, but it relies on UIR as its human-decision
substrate.

## Decision

> Run one bounded UIR Closure Gate before SAVR.

The closure gate fixes only violations of the already-approved UIR contract.

It does not expand the product and does not absorb semantic-authority work that
belongs to SAVR.

## Why before SAVR

SAVR needs trustworthy answers to:

- what fact is actually unresolved?
- can the human resolve it from the current surface?
- does every surface reach the same finalizer?
- did a human interaction happen because semantics were unresolved, or because a
  channel implementation was incomplete?
- are product metrics describing the true interaction path?

Leaving UIR drift in place would contaminate SAVR measurements and force SAVR to
change a moving review substrate.

## Rejected alternatives

### Start SAVR immediately

Rejected. It would build semantic-authority changes on top of still-duplicated
review mutations and misleading review metrics.

### Reopen UIR as another broad sprint

Rejected. Most of UIR is delivered. Broad cleanup would delay the higher-value
SAVR work and invite scope creep.

### Hotfix each symptom independently

Rejected. The remaining gaps share existing product contracts and must be closed
through those contracts, especially ADR-046 and the canonical ReviewDecision
model.

### Build full Wealth snapshot editing in Telegram

Rejected as YAGNI. Snapshot authoring is a richer Wealth workflow, not required
to prove the current review blocker is actionable.

## Boundary

UIR Closure owns:

- mandatory Web escape removal for active review blockers;
- shared canonical review resolution parity;
- canonical Inbox read authority for current producers;
- truthful review telemetry;
- structural producer-to-capability gating.

SAVR owns:

- semantic authority;
- validator consequence;
- known-fact preservation;
- representation adequacy;
- cross-source learned-fact reuse;
- residual fidelity caused by semantic validation;
- domain continuity beyond the already-shared review finalizers.

## Exit

When the UIR Closure PRD Definition of Done is met on merged main, UIR is frozen
and SAVR starts.

No additional UIR cleanup should be accepted after that point unless it fixes a
production defect against the frozen contract.
