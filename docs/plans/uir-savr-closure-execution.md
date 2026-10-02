# UIR-SAVR Closure Sprint Execution Plan

**Status:** CLOSED (2026-10-03, UISC-04)  
**Baseline:** `main@f6b2d374fe7c45bdb8d69507c39596f6945e906a`  
**PRD:** `docs/RICHMOD_UIR_SAVR_CLOSURE_PRD.md`  
**BDR:** `docs/bdr/BDR-005-uir-savr-closure-before-ceu.md`

## Operating rule

This is one bounded closure sprint. Respect repository review FIFO.

For every implementation slice:

1. fetch exact latest `main`;
2. re-audit the named call sites;
3. make the smallest durable change;
4. add the narrow regression/integration test;
5. run the combined drift guard;
6. bundle fixes before pushing;
7. wait for review of the exact latest PR head before another push.

CEU may begin only after UISC-04, which is now recorded.

---

## UISC-00 — exact-main closure audit

Confirm:

- bank `projectSourceReview` still gates on Telegram source payload;
- financial-email `projectReviewItem` still gates on Telegram source payload;
- `telegram.ProjectReviewItem` still resolves active household recipients;
- SAVR Operations still reports the three closure metrics as coverage gaps;
- no newer main commit already changed these contracts.

Record the exact baseline SHA and call sites in the implementation PR.

Exit: assumptions are current.

---

## UISC-01 — email-origin household projection

Implementation: both email producers call the existing `telegram.ProjectReviewItem`
with the canonical household and `originatingChatID=0`; household membership and
active Telegram identity remain the recipient authority. Non-Telegram source
payload is no longer a projection gate. Disposable PostgreSQL tests assert one
recipient/send job on retry for both source families; production delivery remains
subject to UISC-03 observation.

The universal projector also checks that the item is still open and, for
document-review actions, has a real document binding. Bank emails with the
legacy `DOCUMENT_EXTRACTION_LOW_CONFIDENCE` reason remain Inbox-actionable but
are not sent a document-only Telegram card that cannot complete the review.

### Bank email

Remove the source-origin Telegram requirement before universal projection.

Required shape:

```text
review created
-> ProjectReviewItem(household, item, originatingChatID=0)
-> universal household recipient resolution
```

### Financial email

Apply the same rule to observation-scoped reviews.

### Tests

Use disposable PostgreSQL fixtures:

- one household;
- one active household member;
- one active Telegram identity;
- BANK_EMAIL / FINANCIAL_EMAIL source payload with no Telegram chat object;
- actionable review;
- assert one projection/recipient/send job;
- repeat and assert idempotency.

Keep existing multi-recipient race/first-valid-write tests green. Do not create a
new enterprise matrix.

Exit: email source provenance no longer controls review deliverability.

---

## UISC-02 — SAVR observability completion

### A. Validator-induced human review

Persist the minimum provenance needed to know whether validation requested a
dimension already accepted at that boundary.

Prefer existing ReviewDecision fields/decisionProvenance.

Operations must expose:

- eligible reviews;
- validator-induced reviews;
- rate;
- historical/unknown coverage indicator where applicable.

### B. Semantic re-decision

Extend existing intelligence-phase provenance minimally so a phase can state
which dimensions were accepted at entry or which dimensions it re-decided.

Operations must expose:

- eligible semantic phases/events;
- re-decision count;
- rate;
- historical coverage indicator.

Independent evidence verification must not be misclassified as semantic
re-decision.

### C. Residual Contract Fidelity

Derive a deterministic structural metric from ReviewDecision + resolution
telemetry.

At minimum detect:

- known non-null fact also declared missing;
- human-supplied field outside the declared residual, except explicit correction;
- unresolved field required during completion but absent from contract;
- quality-only/non-material consequence represented as human residual.

Expose eligible, violations, and rate/ratio.

### D. Existing metrics

Keep RHICE, known-fact re-ask, auto-confirm correction, review round trips,
bounded choices, and calls/event intact.

Remove the three permanent `notYetMeasurable` entries only when their real
aggregates exist.

Exit: Operations can measure all closure metrics prospectively.

---

## UISC-03 — exact-SHA verification and production observation

Before deploy:

- disposable PostgreSQL migrations;
- API + worker `go test ./...`;
- API + worker `go vet ./...`;
- frontend/container/security checks;
- SAVR corpus;
- UIR review projection/actionability regressions;
- combined drift guard.

After deploy:

- record exact production SHA;
- do not seed fake finance data;
- use Richmod normally;
- verify Operations metrics are available and historically honest;
- when a natural email-origin actionable review occurs, verify Telegram
  projection reaches the eligible household identity;
- inspect real corrections/reviews for semantic regression;
- use kill switches if a bounded auto-confirm lane proves unsafe.

Source families not naturally observed are recorded as
`PRODUCTION_UNOBSERVED` with corpus evidence.

Exit: no observed contradiction to UIR/SAVR north stars, and product owner
accepts real-use behavior.

---

## UISC-04 — docs freeze and CEU handoff

Update:

- SAVR-09 -> product-complete;
- SAVR-10 -> full freeze;
- S08-08 -> closed;
- UIR closure -> post-SAVR projection defect closed;
- Operations coverage docs -> measured/coverage-incomplete as applicable.

Final statement:

```text
UIR frozen
SAVR frozen
owner-household production observation accepted
CEU may start
```

Stop closure work.
