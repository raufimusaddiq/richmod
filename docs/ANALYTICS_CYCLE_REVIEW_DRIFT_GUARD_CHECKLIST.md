# Analytics Cycle Review Drift Guard Checklist

Use this checklist before marking any Analytics Cycle Review implementation task complete.

Source contract:

- `docs/RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md`
- `docs/bdr/BDR-006-analytics-household-cycle-review.md`
- `docs/plans/analytics-cycle-review-sprint.md`
- `docs/bdr/BDR-001-minimum-human-interaction-single-pass-intelligence.md`
- `docs/adr/ADR-015-aggregate-only-llm-insights.md`

A failed item blocks completion unless the implementation PR explicitly documents why it does not apply and the deviation is approved.

---

## A. Product purpose

- [ ] Does the change make `/analytics` more useful for an actual household cycle review?
- [ ] Can the household answer "what happened?" from deterministic data?
- [ ] Can the household answer "what materially changed?" without reading every transaction?
- [ ] Can the household inspect what drove a material change?
- [ ] Can the household distinguish previous-cycle noise from a recent baseline where history exists?
- [ ] Are Savings and Wealth connected to the cycle review without turning Analytics into a duplicate Wealth page?
- [ ] Does the page remain useful if AI is completely unavailable?
- [ ] Did the change avoid reintroducing budgeting-first behavior?

Red flag:

~~~text
new AI copy or new chart
without a clearer household decision/review job
~~~

---

## B. Deterministic ownership

- [ ] Are cycle boundaries computed server-side?
- [ ] Are income, expense, refund adjustment, and net cashflow server-owned?
- [ ] Are transfers excluded according to current canonical policy?
- [ ] Are baselines server-owned?
- [ ] Are medians/averages server-owned?
- [ ] Are category and merchant deltas server-owned?
- [ ] Is contribution-to-change server-owned?
- [ ] Are rankings and top drivers server-owned?
- [ ] Is materiality decided by versioned backend policy?
- [ ] Is data-quality state server-owned?
- [ ] Are Wealth reconciliation values server-owned?
- [ ] Does browser JavaScript only render/format rather than invent authoritative finance calculations?

If any visible number exists only because the LLM wrote it, drift has occurred.

---

## C. Baseline integrity

- [ ] Is the selected cycle explicit?
- [ ] Is the previous comparison a completed comparable cycle?
- [ ] Is the 3-cycle baseline built only from completed eligible cycles?
- [ ] Is insufficient history represented explicitly?
- [ ] Are zero/near-zero baseline cases handled without misleading percentages?
- [ ] Is a dramatic previous-cycle delta contextualized when the recent median says the current value is normal?
- [ ] Are future days/periods excluded from active-cycle baseline math?

Red flag:

~~~text
"up 200%"
because the denominator was tiny
and no absolute/material context is shown
~~~

---

## D. Materiality integrity

- [ ] Is materiality implemented in Go/SQL or another deterministic server-owned policy?
- [ ] Is the materiality policy versioned?
- [ ] Does the policy consider absolute impact?
- [ ] Does it avoid treating tiny changes as important from percentage alone?
- [ ] Can the API explain why a candidate qualified using bounded reason codes?
- [ ] Are low-value candidates omitted?
- [ ] Can "no material finding" occur without error?

The prompt is not a valid place to hide materiality thresholds.

---

## E. Native-tool-only AI

This section is mandatory whenever generative AI is touched.

- [ ] Does the model call go only through LiteRouter / the existing gateway?
- [ ] Does the code use native tool calling?
- [ ] Is tool choice required?
- [ ] Is the tool schema server-owned?
- [ ] Is the schema strict?
- [ ] Is `additionalProperties=false` used where supported?
- [ ] Are unknown fields rejected?
- [ ] Is the expected tool name validated?
- [ ] Is free-form model output rejected as a product result?
- [ ] Is there no direct browser-to-model request?
- [ ] Is there no direct provider call?
- [ ] Does gateway failure avoid a free-form fallback?
- [ ] Does malformed tool output fail closed to deterministic Analytics?
- [ ] Does `scripts/check_native_only_llm.sh` cover the changed Analytics path, or is equivalent enforcement added?

Hard prohibition:

~~~text
AI requested
-> free-form chat/prose response
-> display it in Analytics
~~~

unless a future explicit ADR changes the repository-wide native-tool standard.

---

## F. AI input boundary

- [ ] Does the model receive only approved structured analytical facts?
- [ ] Are raw transaction rows excluded?
- [ ] Is raw bank/email/document evidence excluded?
- [ ] Are raw Telegram/user messages excluded?
- [ ] Are canonical transaction/account UUIDs excluded?
- [ ] Are credentials/secrets excluded?
- [ ] Are fact references semantic rather than database IDs?
- [ ] Are merchant/category display values included only when already suitable for household presentation?
- [ ] Is the fact packet bounded to the selected cycle/relevant baseline?

Red flag:

~~~text
"the model can find the insight if we give it the whole ledger"
~~~

---

## G. AI output boundary

- [ ] Is the output structured rather than one unbounded narrative?
- [ ] Does every finding have at least one evidence ref?
- [ ] Does every evidence ref exist in the approved fact packet?
- [ ] Are unsupported refs rejected?
- [ ] Is finding count bounded?
- [ ] Are text lengths bounded?
- [ ] Are finding kinds enum-constrained?
- [ ] Is there no mandatory recommendation/advice field?
- [ ] Can the model return `no_material_finding`?
- [ ] Are monetary/percentage values rendered from deterministic facts rather than trusted from prose?
- [ ] Can every visible AI finding expose its supporting data?

Hard prohibition:

~~~text
AI says "Groceries rose Rp620k"
but the UI cannot show which deterministic fact supports Rp620k
~~~

---

## H. Intelligence efficiency and ownership

- [ ] If deterministic materiality finds no signal, is the generative call skipped?
- [ ] If a bounded Jev verifier is used, does it answer a distinct bounded question?
- [ ] Does generative inference avoid repeating a Jev-owned decision?
- [ ] Does Jev avoid repeating a generative decision solely for consensus?
- [ ] Are model calls reused/persisted appropriately for the same stable closed-cycle fact snapshot?
- [ ] Does refreshing the page avoid unnecessary generation?
- [ ] Is AI generation asynchronous where current architecture expects it?
- [ ] Does Analytics remain responsive while AI is pending?

Follow BDR-001: cheapest sufficient intelligence once; escalate only for residual uncertainty.

---

## I. Chart usefulness

For every chart touched:

- [ ] What question does this chart answer?
- [ ] Is a chart materially better than a table/text for that question?
- [ ] Are values deterministic?
- [ ] Is the selected period obvious?
- [ ] Is the baseline/comparison obvious?
- [ ] Does the chart avoid future-zero distortion?
- [ ] Does it have a useful empty state?
- [ ] Is the tooltip understandable in IDR?
- [ ] Does it work on tablet/mobile?
- [ ] Is there adjacent context that explains why the chart matters?
- [ ] Can the user drill into supporting data where appropriate?

Reject charts whose only justification is "we already have this metric."

---

## J. UI composition

- [ ] Does the page read like a household review rather than an admin dashboard?
- [ ] Is there a clear visual hierarchy instead of identical cards everywhere?
- [ ] Is "What changed" first-class?
- [ ] Is "What drove it" first-class?
- [ ] Are Savings/Wealth contextual rather than duplicated wholesale?
- [ ] Are data-quality blockers visible and actionable?
- [ ] Are AI findings concise and subordinate to facts?
- [ ] Is there no chatbot visual language?
- [ ] Is there no neon/gradient AI treatment?
- [ ] Is there no generic "AI Advice" label?
- [ ] Is the page usable with keyboard and screen-reader semantics?
- [ ] Does the page remain coherent with JavaScript/API partial failures?

---

## K. Household safety and neutrality

- [ ] Does member attribution remain descriptive?
- [ ] Is there no spouse/member score?
- [ ] Is there no "best/worst spender" concept?
- [ ] Is there no shaming language?
- [ ] Does generated copy avoid assigning motives?
- [ ] Does generated copy avoid calling spending "good", "bad", "healthy", or "unhealthy" without explicit household-owned rules?
- [ ] Are discussion questions neutral?
- [ ] Are household decisions created only by explicit household action?
- [ ] Does AI never silently create a household decision?
- [ ] Does Analytics avoid financial/investment/tax/legal advice?

---

## L. Household decisions

When decision-log functionality is touched:

- [ ] Is the decision household scoped?
- [ ] Is the author recorded?
- [ ] Is the save explicit?
- [ ] Is the decision separate from canonical transactions?
- [ ] Is the decision separate from Wealth observations?
- [ ] Is editing/revocation behavior explicit?
- [ ] Is cross-household access tested?
- [ ] Are later comparisons descriptive rather than causal?
- [ ] Does telemetry avoid duplicating raw decision text unnecessarily?

---

## M. Data quality

- [ ] Are concrete blockers shown instead of an opaque model confidence score?
- [ ] Are open reviews linked to Inbox?
- [ ] Is missing/stale Wealth context represented honestly?
- [ ] Does incomplete category coverage avoid unsupported claims?
- [ ] Does missing evidence remain missing?
- [ ] Can the AI layer be suppressed when data quality is insufficient?
- [ ] Does low data quality avoid repeated model retries that cannot improve the facts?

---

## N. Persistence and auditability

- [ ] Is the deterministic input snapshot auditable?
- [ ] Is materiality policy version recorded?
- [ ] Is AI tool/prompt contract version recorded?
- [ ] Is structured AI output persisted separately from authoritative finance state?
- [ ] Are existing historical insight rows preserved?
- [ ] Is compatibility behavior explicit?
- [ ] Are migrations forward-only?
- [ ] Is `docs/DATABASE_SCHEMA.md` updated in the same branch for schema changes?
- [ ] Are financial mutations still owned only by Go?

---

## O. Tests

- [ ] Stable cycle proves no model call is required.
- [ ] Previous-cycle outlier / recent-median-normal scenario is tested.
- [ ] Material category increase and driver contribution are tested.
- [ ] Tiny denominator does not create a misleading material finding.
- [ ] Incomplete data suppresses unsupported analysis.
- [ ] Native tool is required.
- [ ] Wrong tool name fails.
- [ ] Unknown fields fail.
- [ ] Unsupported evidence ref fails.
- [ ] AI gateway failure leaves deterministic Analytics intact.
- [ ] No recommendation field exists in the new contract.
- [ ] Web renders deterministic fallback.
- [ ] Drill-down preserves correct household/period filters.
- [ ] Cross-household decision access fails.
- [ ] Tests assert model-call count/order where relevant.
- [ ] No test was weakened merely to match legacy behavior.

---

## P. Scope guard

- [ ] No budgeting revival.
- [ ] No goals feature bundled into this initiative.
- [ ] No subscription/recurring engine bundled unless separately approved.
- [ ] No portfolio P/L/live-price work.
- [ ] No unrelated Telegram redesign.
- [ ] No provider-specific ingestion branch.
- [ ] No new infrastructure unless separately approved.
- [ ] No Python/Ollama/Redis/Kafka additions.
- [ ] No direct TypeSafe/provider integration outside LiteRouter.
- [ ] No threshold tuning hidden inside an unrelated UI PR.

---

## Q. PR review questions

Before requesting final approval, the implementation PR must answer:

1. What household review question does this PR make easier to answer?
2. Which displayed facts are new, and where are they computed?
3. What comparison baseline is used and why?
4. What materiality policy decides whether something is worth showing?
5. If AI is used, what exact native tool is required?
6. What facts are sent to the model?
7. What facts are intentionally withheld?
8. How is every AI finding bound back to deterministic evidence?
9. What happens when the model is unavailable?
10. What prevents free-form prose from returning later?
11. What test proves a stable cycle does not generate filler?
12. What test prevents a misleading previous-cycle comparison?
13. What part of the page remains useful without AI?
14. Did this PR introduce any new household decision or financial authority? If so, who owns it?

If any answer is vague, the PR is not ready.

---

## Final invariant

The target is:

~~~text
trusted ledger
+ useful deterministic analysis
+ meaningful charts
+ optional native-tool evidence-bound interpretation
+ household-owned discussion and decisions
~~~

not:

~~~text
more charts
+ more AI text
~~~
