# RICHMOD — UIR-SAVR CLOSURE SPRINT

## Product Requirements Document for Codex /goals

**Status:** APPROVED PRODUCT DIRECTION / FINAL PRE-CEU CLOSURE GATE  
**Repository:** raufimusaddiq/richmod  
**Baseline:** `main@f6b2d374fe7c45bdb8d69507c39596f6945e906a`  
**Date:** 2026-09-28  
**Depends on:** UIR PRD + Closure Gate, ADR-046, SAVR PRD, BDR-004, ADR-047, ADR-048  
**Next product initiative after this gate:** CEU

---

# 2026-09-28 blocking owner-household production finding

~~**UISC-03 acceptance and UISC-04 freeze remain blocked. Do not start CEU.**~~

**Resolved (UISC-04, 2026-10-03):** the product owner accepted the owner-household observation and the freeze is recorded in `docs/bdr/BDR-005-uir-savr-closure-before-ceu.md`. The finding below is retained as history.

Real owner-household use showed that an undecided Jev Telegram route terminated
ordinary chat before the conversational agent, and a provider failure routed a
non-keyword chat message to `NEEDS_REVIEW` via Go keyword NLP. Jago bank
email extraction failed schema validation twice with `invalid transaction
time` (exact emitted string not captured). A Bibit provider-email review
had no transfer reconciliation case; Web Ignore failed and required guarded
manual DML. The exact reason Bibit entered review was not proven.

The repair is tracked in
[`docs/audits/CORE-INTELLIGENCE-BOUNDARY-REPAIR.md`](../audits/CORE-INTELLIGENCE-BOUNDARY-REPAIR.md).
It removes semantic Go fallback routing, scopes failed/undecided Jev turns to
conversation + READ-only capabilities, gives bank extraction one specific
schema-feedback repair, and routes observation-scoped Ignore through the
shared observation finalizer. No production success is claimed from tests.

Closure requires real owner-household post-deploy observation: ordinary chat
answered; unclear route and machine outage create no human review/RHICE;
canonical reads use READ tools; simple finance fast path call budgets remain
low; post-auto-confirm material corrections do not rise. Jago and Bibit
rechecks must record actual observed outcomes; tests alone cannot satisfy
this gate. Production observation: **PENDING** — repair deployed to production
as `f702085` (Deploy Production run 36456564122, approved); `/healthz` and
`/readyz` returned 200 and the running images are
`sha-f70208537746a0725750dee53d68f78f55d4b127`. Natural owner-household
traffic has not yet been observed against these checks.

---

# 0. Why this exists

UIR and SAVR are functionally delivered, but the combined post-delivery audit found
two bounded closure defects that must be resolved before CEU:

1. email-origin reviews already have canonical household ownership, but two
   producer adapters still gate Telegram projection on whether the originating
   source event itself contains Telegram chat provenance;
2. SAVR's semantic behavior is corpus-covered and deployed, but three product
   observability promises are not yet measurable and the current SAVR-09 canary
   requirement assumes a disposable household that does not match Richmod's
   current product reality.

This sprint does **not** reopen UIR or SAVR as broad initiatives.

Its objective is:

> close the last interaction-projection defect, make SAVR's north-star promises
> observable from real production use, validate them on the actual owner
> household without synthetic production data, then freeze UIR + SAVR and start
> CEU.

---

# 1. Product reality and testing posture

Richmod is currently a personal production system. The product owner is also the
primary real user, and there is one real household.

Therefore:

- the real household is the production canary;
- real Telegram/email/document usage is the production observation cohort;
- real corrections and repeated questions are first-class product evidence;
- production MUST NOT be seeded with fake transactions, fake household members,
  fake bank emails, or synthetic financial state merely to satisfy a rollout
  checklist;
- correctness tests still use disposable PostgreSQL/integration fixtures;
- corpus tests remain the proof for rare source families or edge cases that do
  not naturally occur during the closure observation period;
- a source family that is not naturally exercised may be recorded as
  `PRODUCTION_UNOBSERVED`; it does not block closure when its contract and
  integration corpus are green.

Do not introduce arbitrary enterprise rollout quotas such as a minimum tenant
count, multi-household cohort, or synthetic traffic volume.

Production observation is **not** permission to weaken deterministic tests.

---

# 2. North-star invariants carried into closure

UIR:

> A canonical review belongs to a household. Surface provenance does not decide
> who is eligible to receive that review.

SAVR:

> Understand once. Use the minimum sufficient intelligence. Preserve accepted
> facts. Validate canonical safety. Ask only for a material residual.

Combined:

> Human interaction is required only when a real household decision is needed,
> and that interaction must reach the eligible household through the available
> review surface without depending on unrelated source provenance.

---

# 3. Closure problem A — email-origin review projection

## 3.1 Current canonical ownership is already sufficient

Cloudflare ingress resolves the recipient local part through
`email_ingress_address`, which already owns `household_id`.

Both bank and financial email ingestion then persist `source_event.household_id`.
Any resulting `review_item` is therefore household-owned before projection.

The universal projector already knows how to resolve eligible Telegram
recipients through:

`review_request.household_id -> household_member -> telegram_identity`.

This is the correct authority chain.

## 3.2 Confirmed defect

Two producer helpers still perform an earlier source-provenance gate:

- `bankemail.projectSourceReview`;
- `financialemail.projectReviewItem`.

They look for a Telegram chat inside the originating `source_event_payload` and
return without calling the universal projector when the source is
`BANK_EMAIL` or `FINANCIAL_EMAIL`.

For Cloudflare-origin email this means:

```text
email -> household known -> review known
     -> no Telegram payload on source event
     -> producer returns early
     -> ProjectReviewItem is never called
     -> Review Inbox works, Telegram projection may be absent
```

This is an implementation defect, not a missing ownership model.

## 3.3 Required behavior

Projection eligibility MUST be household-first:

```text
review_item.household_id
        ↓
review_request.household_id
        ↓
household recipient policy
        ↓
active household_member + telegram_identity
        ↓
review_request_recipient
        ↓
Telegram projection
```

The originating Telegram chat is only a fallback for a genuinely
Telegram-origin review when no eligible household recipient can otherwise be
resolved.

A non-Telegram source MUST NOT suppress projection merely because it has no
`message.chat.id`.

## 3.4 Implementation constraints

Do:

- reuse `telegram.ProjectReviewItem`;
- pass the canonical household and review item;
- let the universal projector resolve recipient identities;
- preserve existing preferred-user/owner/member ordering and first-valid-write
  behavior;
- preserve Web Review Inbox behavior;
- keep source provenance for evidence/audit only.

Do not:

- bind Cloudflare addresses directly to Telegram chat IDs;
- use `email_ingress_address.created_by_user_id` as review-delivery authority;
- create a second recipient table;
- create an email-specific Telegram renderer;
- duplicate `ProjectReviewItem` recipient queries in bank/financial-email code.

## 3.5 Required tests

At minimum:

1. bank email source event with no Telegram payload, known household, one active
   eligible Telegram identity -> review projects to that identity;
2. financial email source event with no Telegram payload, known household, one
   active eligible Telegram identity -> review projects to that identity;
3. no duplicate `review_item`, `review_request`, or recipient on retry;
4. stale/closed review is not reactivated by projection;
5. existing Telegram-origin fallback remains valid;
6. existing multi-recipient/first-valid-write regressions stay green.

The primary acceptance fixture SHOULD model the current product: one household,
one active user/Telegram identity. Do not invent a multi-tenant rollout matrix.

---

# 4. Closure problem B — SAVR product observability

SAVR currently measures RHICE, known-fact re-asks, review turns, bounded choices,
auto-confirm corrections, and intelligence phase counts. Three promised signals
remain explicitly `notYetMeasurable`.

The closure sprint must make the north-star properties measurable without
building a new semantic platform.

## 4.1 Validator-Induced Human Review Rate

Question:

> Did a deterministic validation consequence create human work even though the
> affected material fact had already been accepted?

A review is validator-induced when the stored decision/provenance shows that:

- the review was caused by validation;
- the blocking dimension was already accepted/known at the validation boundary;
- no independent material conflict, canonical ambiguity, hard invariant, or
  human policy requirement justifies the human turn.

Required instrumentation:

- record enough accepted-dimension + validation-consequence provenance at the
  review boundary to distinguish a legitimate residual from validator-created
  rework;
- derive the rate in Operations from production state;
- do not record raw evidence values merely for this metric.

Prefer existing `ReviewDecision.decisionProvenance`,
`validationConsequence`, `affectedFacts`, `knownFacts`, and
`missingFacts`. Add the smallest additive field only if these cannot represent
the invariant honestly.

## 4.2 Semantic Re-decision Rate

Question:

> Did a later intelligence phase decide a semantic dimension that was already
> accepted before that phase began?

The current `intelligence_phase_telemetry` records semantic, answered, and
residual dimensions but does not record whether a dimension was already accepted
at phase entry.

Required instrumentation must make this deterministically queryable.

Acceptable minimal shapes include:

- `accepted_dimensions_at_entry`; or
- `redecided_dimensions`;

on the existing phase telemetry contract, or an equivalent minimal provenance
field on an existing record.

Rules:

- an independent evidence check is not semantic re-decision merely because it
  references the same fact;
- a bounded residual call is not re-decision when the dimension was genuinely
  unresolved;
- user confirmation followed only by canonical validation must not count as
  re-decision;
- the metric must be derivable without reading prompts/raw financial values.

## 4.3 Residual Contract Fidelity

The previous `Residual Fidelity Rate` wording assumed an external labelled
semantic ground-truth set. That is unnecessary and too lab-oriented for current
Richmod.

For closure, the product metric is **Residual Contract Fidelity**:

> Does the stored ReviewDecision ask only for dimensions that are genuinely
> unresolved by its own accepted-fact, conflict, policy, and consequence
> contract, and does resolution require only those declared residual inputs?

A review contract is faithful when all applicable properties hold:

- a non-null known fact is not also listed as missing;
- blocking `missingFacts` are explained by the stored consequence,
  conflict/ambiguity, or human-policy contract;
- human-supplied fields do not exceed the declared residual except for explicit
  correction actions;
- a review does not require a second undeclared semantic field to finish;
- a quality-only/non-material signal is not represented as a human residual;
- resolution through another surface does not silently add semantic requirements.

This metric is intentionally structural and production-measurable. It replaces
the unmeasurable labelled-ground-truth interpretation for the current product
stage.

No user labelling workflow is required.

## 4.4 Operations contract

After implementation, Operations must no longer report these closure metrics as
permanent `notYetMeasurable` gaps:

- validator-induced human review;
- residual contract fidelity;
- semantic re-decision.

Historical rows that predate the required provenance may be reported as
`coverageIncomplete` / excluded from the eligible denominator. Do not convert
unknown historical coverage into zero.

No new dashboard is required. Existing Operations/Admin surfaces may expose the
new fields through the smallest extension.

---

# 5. Production observation gate — owner household is the canary

The production canary is the actual owner household.

## 5.1 What to do

After the closure implementation is merged and deployed:

- continue normal Richmod usage;
- observe naturally occurring Telegram, bank-email, financial-email, receipt,
  screenshot, payslip, and review events;
- use existing kill switches for bounded auto-confirm rollback if a real
  regression appears;
- inspect Operations metrics and actual review/correction history;
- treat any owner-observed repeated question, wrong canonicalization, or
  unnecessary review as a real closure finding.

No fake production event is required.

## 5.2 Evidence policy

Closure evidence is three-layered:

```text
contract/integration tests
        +
existing SAVR regression corpus
        +
real owner-household production observation
```

For metrics added by this sprint, do not fabricate a pre-SAVR historical
baseline if old rows lack the necessary provenance. Mark the historical boundary
honestly and measure prospectively from the instrumentation deployment.

For already-existing metrics, historical real production data may be used when
the cohort definition is comparable.

## 5.3 No arbitrary volume gate

There is no required number of households or fake events.

Closure is blocked by **observed contradiction**, not by absence of enterprise
sample size.

A routinely used source family should be checked when it naturally occurs.
A rare family may remain `PRODUCTION_UNOBSERVED` with green corpus evidence.

The product owner provides the final real-use acceptance signal because the
current product is explicitly built for and operated by that household.

---

# 6. Sprint tasks

## UISC-00 — exact-main re-audit

Before code changes:

- fetch latest `main`;
- verify the two email projection pre-gates still exist;
- verify `ProjectReviewItem` still resolves recipients by household;
- verify the three SAVR metrics are still coverage gaps;
- record any changed call site before implementation.

Exit: no task starts from stale line-level assumptions.

## UISC-01 — household-owned email review projection

Implement §3 for both bank and financial email review producers.

Exit:

- non-Telegram email review can project to eligible household Telegram identity;
- no email-specific recipient architecture is added;
- retries remain idempotent;
- canonical review ownership and first-valid-write semantics are unchanged.

## UISC-02 — minimum SAVR observability completion

Implement §4.

Exit:

- Validator-Induced Human Review Rate is deterministically measurable for
  provenance-eligible rows;
- Semantic Re-decision Rate is deterministically measurable for
  provenance-eligible phases;
- Residual Contract Fidelity is deterministically measurable;
- Operations clearly separates unknown historical coverage from measured zero;
- no raw sensitive evidence is added solely for metrics;
- no new semantic engine/table is introduced without a demonstrated storage
  impossibility.

## UISC-03 — owner-household production observation

Deploy the exact reviewed SHA and use normal real flows.

Required checks:

- email-origin review projection reaches the household Telegram recipient when a
  natural applicable review occurs;
- Operations returns the new metrics without pretending unknown history is zero;
- no observed SAVR regression increases unnecessary human work;
- any auto-confirm correction remains attributable by source/field;
- kill-switch rollback remains available.

Do not seed fake financial data.

## UISC-04 — final freeze and CEU handoff

After code, corpus, deployment, and owner observation are accepted:

- mark SAVR-09 product-complete under the single-household observation model;
- mark SAVR-10 full freeze;
- close S08-08;
- update UIR closure status to include the household-recipient projection fix;
- remove obsolete `notYetMeasurable` entries that are now measurable;
- retain explicit historical coverage caveats;
- declare UIR + SAVR frozen;
- start CEU.

Stop the sprint here. Do not continue opportunistic cleanup.

---

# 7. Definition of Done

This combined closure sprint is complete only when all are true:

1. Cloudflare email ownership resolves through `email_ingress_address.household_id`
   and remains canonical through `source_event` / `review_item`;
2. bank-email review projection does not require originating Telegram payload;
3. financial-email review projection does not require originating Telegram
   payload;
4. `ProjectReviewItem` remains the single recipient/projection path;
5. eligible recipient resolution remains household-scoped and server-owned;
6. existing Review Inbox behavior is unchanged;
7. Validator-Induced Human Review Rate is measurable for new eligible rows;
8. Semantic Re-decision Rate is measurable for new eligible phases;
9. Residual Contract Fidelity is measurable without a manual labelling workflow;
10. historical missing provenance is reported as unknown/incomplete, not zero;
11. RHICE, known-fact re-ask, correction, review, and call telemetry remain green;
12. full SAVR corpus passes on disposable test infrastructure;
13. no fake financial state is inserted into the real household for closure;
14. production validation uses normal owner-household usage;
15. source families not naturally encountered may be explicitly
    `PRODUCTION_UNOBSERVED` rather than blocking forever;
16. any observed real semantic regression blocks closure until corrected;
17. no new model call is introduced merely for telemetry or projection;
18. no new review/semantic framework is introduced;
19. SAVR-10 is moved from partial to full freeze only after the above evidence is
    recorded;
20. CEU does not start before this gate is accepted.

---

# 8. Acceptance scenarios

## A1 — Cloudflare bank review reaches Telegram

Given a Cloudflare bank email whose local part belongs to household H, and H has
one active Telegram identity, when processing creates an actionable review, the
review is projected to that Telegram identity even though the BANK_EMAIL source
payload contains no Telegram chat object.

## A2 — Cloudflare financial-email review reaches Telegram

Same as A1 for FINANCIAL_EMAIL and its observation-scoped review.

## A3 — retry is idempotent

Re-running either projection path creates no duplicate canonical review,
projection request, or recipient.

## A4 — household authority, not source provenance

A review with household H and a non-Telegram source is deliverable. A source
payload that lacks a chat ID is not treated as evidence that H has no Telegram
recipient.

## A5 — validator-induced review measurement

A review whose stored contract shows a genuinely unresolved material dimension
is not counted as validator-induced. A regression fixture where validation
re-asks an already accepted dimension is counted.

## A6 — semantic re-decision measurement

A residual Jev call over a still-unresolved category is not counted. A later
semantic phase that re-decides a category already accepted at phase entry is
counted.

## A7 — residual contract fidelity

Known `amount/date/category` with `missingFacts=["category"]` fails fidelity.
Known `amount/date`, missing `category`, and human supplying only category
passes.

## A8 — historical telemetry honesty

Rows predating new provenance are excluded or marked coverage-incomplete. They do
not produce an artificial zero rate.

## A9 — real owner canary

After deployment, ordinary real usage generates telemetry without a second
household or seeded transactions. A real correction or repeated known-fact
question becomes a closure finding.

---

# 9. YAGNI / anti-enterprise guardrails

Do not add:

- disposable production households;
- tenant cohorts;
- experiment assignment;
- synthetic production financial events;
- rollout percentages;
- a generic observability platform;
- a new event bus;
- a semantic ontology/fact database;
- a human labelling tool;
- a separate Telegram recipient service;
- a new review subsystem;
- new AI calls to evaluate whether SAVR worked.

Richmod may become multi-household later. That future product state can amend the
rollout methodology then. This sprint must optimize for the product that exists
today.

---

# 10. Programmer completion report

Every implementation PR/task must report:

```text
Task:
Baseline main SHA:
Closure requirement:
Active call sites changed:
Canonical ownership source:
Recipient resolution path:
Existing abstraction reused:
Schema/storage change:
Why storage change is necessary:
Metric definition:
Eligible denominator:
Historical coverage caveat:
Production data written:
New model calls:
Tests:
Corpus:
Production observation evidence:
Drift guard:
Known follow-up:
```

Any answer that introduces new semantic authority or a second projection
authority must be rejected before merge.
