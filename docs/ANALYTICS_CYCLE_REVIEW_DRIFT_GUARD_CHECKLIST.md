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
- [ ] Are explicit deterministic product rules versioned where applicable, while open-ended analytical significance remains intelligence-owned?
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

## D. Analytical ownership

- [ ] Does Go compute objective financial measurements rather than semantic conclusions?
- [ ] Are amount, delta, baseline, contribution, concentration, and ordering deterministic?
- [ ] Is open-ended "what is noteworthy?" owned by generative intelligence?
- [ ] If Jev decides relevance, is the question genuinely bounded?
- [ ] Is there no growing threshold tree whose real purpose is to imitate analyst judgment?
- [ ] Is there no keyword/regex/switch logic deciding what a household should discuss?
- [ ] Can the model conclude that nothing noteworthy stands out without being forced to fill N slots?

Red flag:

~~~text
if delta > X && share > Y {
    insight = "This category is important"
}
~~~

unless X/Y are an explicit deterministic product rule whose purpose is not
semantic imitation.

---

## E. Native analytical data-tool boundary

This section is mandatory whenever AI analysis is touched.

- [ ] Are financial data/state requests satisfied through native analytical READ tools?
- [ ] Are tool names allow-listed by server/channel state?
- [ ] Are tool argument schemas strict and server-owned?
- [ ] Are unknown/malformed analytical tool calls rejected?
- [ ] Is there no "return JSON matching this schema" prompt contract for analysis?
- [ ] Is there no JSON embedded in prose that Go parses as a financial-analysis contract?
- [ ] Is there no direct browser-to-provider or direct provider bypass around LiteRouter?
- [ ] Are the analytical READ implementations reusable by both Web and Telegram rather than duplicated?
- [ ] Does `scripts/check_native_only_llm.sh` cover any new Analytics model path, or is equivalent enforcement added?

The structured machine contract is the analytical tool arguments/results.
Presentation is channel-specific.

---

## F. Data access boundary

- [ ] Does the model obtain finance data through bounded Richmod tools?
- [ ] Is there no unrestricted database/ledger dump tool?
- [ ] Are raw email/document/provider payloads excluded unless a dedicated extraction lane explicitly needs source evidence?
- [ ] Are credentials/secrets excluded?
- [ ] Are canonical IDs hidden when server-scoped references can bind the target?
- [ ] Are tool results household-scoped and authorization-checked?
- [ ] Are transaction details returned only when a bounded analytical tool actually needs them?
- [ ] Are tool results factual/structured rather than pre-written Go narratives?
- [ ] Can dependent READs be performed through the bounded model loop instead of Go guessing which data the model "must" need?

Red flag:

~~~text
Go pre-decides the answer
-> gives model only the facts that support that answer
~~~

---

## G. Rendering/output boundary

- [ ] Does model-written analysis finish through a native rendering/respond tool?
- [ ] Is the render tool display-only?
- [ ] Is its prose field intentionally free-form?
- [ ] Does Go validate the tool envelope but avoid NLP-parsing the message?
- [ ] If supporting refs are accepted, are they bounded and server-issued?
- [ ] Are UI-critical amounts rendered from deterministic data/tool results?
- [ ] Is the final prose never parsed into a transaction, decision, category, amount, materiality flag, or household state?
- [ ] Is there no mandatory recommendation/advice field?
- [ ] Can the render message simply say that nothing noteworthy stands out?
- [ ] Does a failed render/model phase leave deterministic Analytics available?

Hard prohibition:

~~~text
model -> prose/JSON text
Go -> parse meaning
Go -> mutate/render structured financial state
~~~

---

## H. Anti-Go semantic drift

- [ ] Did the PR add `strings.Contains`, keyword maps, or regexes to interpret ordinary human language?
- [ ] Did the PR add switch/case branches that choose analytical meaning or narrative?
- [ ] Did the PR add canned templates presented as AI analysis?
- [ ] Did the PR add arbitrary thresholds whose actual job is to decide open-ended noteworthiness?
- [ ] On model/gateway failure, does the code fail/defer/review rather than imitate the model in Go?
- [ ] If Go authors text, is it a literal protocol/status/error acknowledgement rather than semantic analysis?
- [ ] Before adding a semantic Go branch, did the implementation explicitly consider Jev for bounded judgment or generative intelligence for open-ended reasoning/prose?
- [ ] Are tests present that fail if the old semantic fallback is reintroduced?

Use this decision test:

> If the branch must understand what a human sentence means, decide what is
> noteworthy, or write an analytical narrative, it probably does not belong in
> Go. Go may enforce exact deterministic policy; it must not imitate intelligence.

Provider failure does not transfer semantic ownership to Go.

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
- [ ] Are deterministic analytical calculations/versioning recorded where needed?
- [ ] Is AI tool/prompt contract version recorded?
- [ ] Is persisted AI commentary clearly non-authoritative text rather than a JSON domain contract?
- [ ] Are existing historical insight rows preserved?
- [ ] Is compatibility behavior explicit?
- [ ] Are migrations forward-only?
- [ ] Is `docs/DATABASE_SCHEMA.md` updated in the same branch for schema changes?
- [ ] Are financial mutations still owned only by Go?

---

## O. Tests

- [ ] Stable cycle proves the model can render a concise no-noteworthy response without filler.
- [ ] Previous-cycle outlier / recent-median-normal scenario is tested.
- [ ] Material category increase and driver contribution are tested.
- [ ] Tiny denominator is exposed with correct deterministic context and does not require a hard-coded Go "importance" conclusion.
- [ ] Incomplete data suppresses unsupported analysis.
- [ ] Every LLM phase requires native tools.
- [ ] Wrong/unexposed tool name fails.
- [ ] Unknown tool-argument fields fail.
- [ ] Raw final assistant text fails.
- [ ] Native RENDER with natural prose succeeds.
- [ ] Supporting refs, when used, are validated.
- [ ] AI gateway failure leaves deterministic Analytics intact.
- [ ] No structured recommendation/advice DTO is required for the rendered response.
- [ ] No regex/keyword/switch/template semantic fallback is introduced in Go.
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
4. Which layer decides what is noteworthy, and why is that not hidden in Go heuristics?
5. What native READ tools can the model use?
6. What native RENDER/respond tool carries the final natural prose?
7. What data is available through tools, and what is intentionally withheld?
8. How can the user inspect the deterministic facts behind the rendered analysis?
9. What happens when the model is unavailable?
10. What prevents JSON-in-text contracts, raw final model text, or Go semantic fallbacks from returning later?
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
+ native-tool analytical reasoning and rendering
+ household-owned discussion and decisions
~~~

not:

~~~text
more charts
+ more AI text
~~~
