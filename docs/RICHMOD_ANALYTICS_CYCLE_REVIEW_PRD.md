# Richmod Analytics Cycle Review PRD

**Status:** Proposed product contract for implementation  
**Target:** latest `main` at implementation time  
**Primary surface:** `/analytics`  
**Product owner intent:** make captured household financial data useful for an end-of-cycle meeting, not merely visible  
**Supersedes where conflicting:** `docs/RICHMOD_ANALYTICS_LLM_INSIGHT_UI_CODEX.md` and older analytics insight/checklist guidance

## 1. North-star fit

Richmod exists to turn messy household financial evidence into trustworthy, explainable household financial state with the least necessary bookkeeping work.

Once the ledger is trustworthy, Analytics must complete the second half of that promise:

> Turn trusted household financial state into a review that two household members can use to understand the cycle, discuss meaningful changes, and record their own decisions.

Analytics is not a budgeting coach, an investment adviser, or an AI chat surface.

The target household job is:

> "Our salary cycle ended. We want to sit down together, understand what actually happened, what was materially different, where the surplus went, how wealth changed, and what we want to remember for the next cycle."

## 2. Problem with the current Analytics surface

The current page already has useful deterministic charts and an LLM insight card, but it is still organized primarily as:

~~~text
KPIs
-> chart
-> generic narrative
-> category ranking
-> merchant ranking
-> member ranking
~~~

This exposes data but does not reliably answer the household questions that matter in a cycle review:

- Did spending materially change, or was the difference noise?
- What actually drove the change?
- Was one category unusual only versus last cycle, or also versus a recent baseline?
- Which merchants or large transactions explain the movement?
- Is the increase broad and recurring-looking, or concentrated in a few events?
- Where did cycle surplus go?
- Did net worth move consistently with confirmed cashflow?
- Is the data complete enough to discuss confidently?
- What should the household discuss, without Richmod deciding for them?
- What did the household decide to remember?

The current generative insight contract also forces prose even when the supplied aggregates do not justify a useful finding. That creates template-like output and weakens trust.

## 3. Product goals

### P0 — Make every visible analytical statement explainable

Every chart, KPI, comparison, and AI-assisted finding must be traceable to deterministic household facts.

No displayed number may originate from generative text.

### P0 — Make cycle review the primary Analytics workflow

`/analytics` must support:

1. current-cycle monitoring;
2. closed-cycle review;
3. comparison with recent cycles;
4. drill-down to supporting transactions;
5. savings and Wealth reconciliation;
6. household discussion prompts grounded in facts;
7. household-authored decision notes.

### P0 — Eliminate AI slop

The AI layer must:

- use Richmod-native read-only tools whenever it needs financial data or state;
- never be prompted to manufacture JSON/structured output for Go to parse;
- receive authoritative facts through tool results, not direct database access;
- be free to write natural user-facing analysis after it has the facts;
- never make free-form prose the source of financial state, arithmetic, materiality, or mutation;
- be allowed to return no material finding;
- never invent causes;
- never shame, score, rank, or judge household members;
- never produce investment, credit, tax, legal, or prescriptive financial advice.

Structured data belongs in tool arguments/results and deterministic UI APIs. Natural language belongs to the model.

### P1 — Make the page usable as a household meeting

A closed cycle should have a focused "Review cycle" flow that can be opened on a laptop/tablet and followed section by section.

The review remains useful when the AI gateway is unavailable.

## 4. Non-goals

This initiative does not add:

- category budgets or spending limits;
- live market prices;
- per-security investment P/L;
- financial advice;
- automatic household decisions;
- AI-written canonical transactions;
- AI direct database access;
- raw ledger access from the model;
- generic chat inside Analytics;
- arbitrary "health scores" for household finances;
- spouse-versus-spouse performance scoring;
- synthetic explanations for missing evidence.

## 5. Product principles

### 5.1 Facts first, interpretation second

The information hierarchy is:

~~~text
canonical ledger / Wealth state
-> deterministic analytical facts
-> deterministic charts and comparisons
-> material finding candidates
-> optional AI interpretation
-> human discussion
-> human decision
~~~

### 5.2 A chart must answer a question

Charts remain first-class. The revamp does not remove them.

Each chart must have a defined analytical job and a deterministic explanation strip or drill-down context.

### 5.3 Comparison must be contextual

"Up versus previous cycle" is not automatically meaningful.

Where enough history exists, Richmod should compute:

- current cycle;
- immediately previous comparable cycle;
- median of the previous 3 completed cycles;
- optional median of the previous 6 completed cycles where useful.

The backend chooses and exposes comparison facts. The AI does not select a convenient baseline by itself.

Example:

~~~text
Dining
current: 1.40m
previous: 0.80m
3-cycle median: 1.35m
~~~

The useful conclusion is not "Dining increased 75%." It is that Dining increased versus the immediately previous cycle while remaining close to its recent baseline.

### 5.4 Materiality before prose

Richmod must compute candidate significance before generative prose is requested.

A candidate may consider deterministic dimensions such as:

- absolute delta;
- relative delta;
- share of total expense;
- contribution to total expense change;
- novelty against recent completed cycles;
- concentration in top merchants/transactions;
- data completeness.

Exact thresholds are implementation policy and must be tested. Do not bury thresholds in prompts.

Small changes should be omitted.

### 5.5 No finding is a valid result

If no candidate is materially useful:

~~~text
No material change detected against the available recent baseline.
~~~

Do not call a generative model merely to fill a card.

## 6. Analytics information architecture

The default page remains `/analytics`.

### 6.1 Cycle selector

The page must expose:

- Current cycle;
- recent closed cycles;
- Calendar analysis as a secondary mode where already supported.

Closed salary cycles are the primary meeting unit.

The selected period must be explicit and URL-addressable.

Recommended query contract:

~~~text
/analytics?view=cycle&cycle=<cycle-start>
/analytics?view=calendar&range=6
~~~

Exact URL design may change if the existing router makes another approach cleaner.

### 6.2 Section 1 — Cycle position

Answer:

- How much came in?
- How much went out?
- What was net cashflow?
- How much confirmed surplus exists?
- How much savings was allocated?
- How much remains unallocated?

Suggested KPI group:

~~~text
Income
Expense
Net cashflow
Allocated savings
Unallocated surplus
~~~

Values come from Go/SQL.

### 6.3 Section 2 — Spending shape

Keep a cycle spending chart.

For active cycle:
- elapsed daily spending only;
- no future-zero distortion.

For closed cycle:
- full cycle daily spending;
- comparison overlay/reference only when units and interpretation remain clear.

Below the chart, show deterministic context such as:

- average daily expense;
- peak day;
- concentration of spending in a short interval;
- comparable recent-cycle baseline.

Do not let AI calculate these.

### 6.4 Section 3 — What changed

This becomes a first-class analytical section.

For each material category:

~~~text
Category
Current amount
Previous-cycle amount
3-cycle median when available
Absolute delta
Relative delta when denominator is meaningful
Contribution to total spending change
~~~

The page should emphasize material changes, not list every category.

A user can expand/drill down to the transactions and merchants that support the change.

### 6.5 Section 4 — What drove the change

For a selected material category, show deterministic drivers:

- top merchants by delta;
- top transactions by amount;
- concentration of the category change;
- whether comparable merchant/category activity appeared in recent cycles.

Use language such as:

> "No comparable transaction at this amount was found in the previous three completed cycles."

Do not automatically label an event "one-off" unless the household explicitly classifies it that way.

### 6.6 Section 5 — Where money went

Keep category and merchant visualizations.

Charts must support the meeting, not duplicate the same ranking several times.

Recommended:
- category distribution/ranking;
- merchant distribution for selected category or whole cycle;
- transaction drill-down.

### 6.7 Section 6 — Household view

Show attribution only where deterministically known.

Allowed labels include:

~~~text
Raufi initiated
Wife initiated
Shared / automatic / unattributed
~~~

Do not transform this into a "who spent more responsibly" comparison.

No ranking, score, or generated judgment of household members is allowed.

### 6.8 Section 7 — Savings and Wealth

Bring together existing Wealth/cycle primitives:

- cycle cashflow surplus;
- confirmed savings allocation;
- savings destinations;
- unallocated residual;
- previous and current net worth snapshot;
- net-worth change;
- confirmed cashflow contribution;
- valuation/other residual already derived by Wealth.

The UI must make clear when a Wealth snapshot is stale or missing.

### 6.9 Section 8 — Data quality / loose ends

Before a closed-cycle meeting is treated as reliable, surface unresolved state:

- open transaction reviews;
- uncategorized confirmed spending where relevant;
- stale/missing Wealth observations required for Wealth comparison;
- incomplete salary/cycle anchor state;
- failed/pending processing that affects the selected cycle, if deterministically knowable.

Do not collapse these into an opaque AI "confidence score."

### 6.10 Section 9 — Discussion points

This is the primary AI-assisted surface.

It must contain at most a small number of material findings.

Each finding contains:

- concise title;
- short interpretation;
- deterministic supporting evidence;
- link/drill-down;
- optional neutral discussion question.

Example:

~~~text
Groceries drove most of the increase

Evidence
Current: Rp2.10m
3-cycle median: Rp1.48m
Delta: +Rp620k
Pamella + Superindo explain Rp510k of the delta.

Discussion
Was this a change in household needs that you expect to continue?
~~~

The amount and percentage rendering is deterministic UI output from evidence references, not model-authored numbers.

### 6.11 Section 10 — Household decisions

A cycle meeting can record household-authored notes/decisions.

Examples:

~~~text
Keep more cash liquid next cycle.
Treat this cycle's vehicle service as unusual maintenance.
Move Rp1.5m of residual savings to emergency reserve.
Cancel subscription X.
~~~

Rules:

- decisions are authored/confirmed by household users;
- AI may never silently create them;
- decisions are not transactions;
- decisions do not mutate historical financial facts;
- later Analytics may display previous decisions alongside measurable facts, but must not claim causation unless established.

A new persisted decision entity is allowed if implementation confirms the data model is necessary. Any schema change requires a migration and same-branch `DATABASE_SCHEMA.md` update.

## 7. Deterministic analytical contract

Introduce a server-owned analytical representation conceptually named `CycleAnalysisFacts`.

This is a contract, not necessarily a single Go struct or API object.

It should cover:

~~~text
period
data_quality
cashflow
spending_shape
category_changes
merchant_drivers
large_transactions
member_attribution
savings
wealth_change
review_state
comparison_baselines
material_candidates
~~~

### 7.1 Fact references

Every analytical fact exposed to the AI layer must have a stable request-local fact reference.

Examples:

~~~text
cashflow.expense.current
cashflow.expense.delta_vs_previous
category.groceries.current
category.groceries.delta_vs_3cycle_median
category.groceries.contribution_to_total_delta
merchant.pamella.groceries_delta
wealth.net_worth.change
savings.unallocated
quality.open_review_count
~~~

These are semantic keys, not database identifiers.

Do not expose canonical transaction UUIDs, account IDs, email addresses, source payloads, or other sensitive identifiers to the model.

### 7.2 Backend ownership

Go/SQL owns:

- amounts;
- date boundaries;
- period selection;
- baselines;
- medians/averages;
- deltas;
- percentages;
- rankings;
- concentration;
- transaction selection;
- merchant/category/member attribution;
- materiality candidate calculation;
- data completeness;
- drill-down query binding.

Browser JavaScript may format and render values but must not become the authoritative source of new financial calculations.

## 8. Tool-first AI contract

This is mandatory.

### 8.1 Tools are for data and actions, not for forcing JSON-shaped answers

When the model needs household financial data, Richmod exposes native provider tools with strict server-owned argument schemas.

Examples:

~~~text
get_cycle_overview
get_material_changes
get_category_drivers
get_merchant_drivers
get_supporting_transactions
get_savings_reconciliation
get_wealth_reconciliation
get_data_quality
~~~

The exact catalog may differ after implementation inspection, but the semantic rule is fixed:

> If the model needs data, give it a tool. Do not ask it to return a structured JSON analysis for Go to parse.

A typical analysis turn is:

~~~text
model
-> native read-only tool call
-> deterministic Go result
-> optional additional native read-only tool call
-> natural assistant prose
~~~

The final prose may be free-form because it is not a machine contract and is never parsed back into financial state.

### 8.2 Native tool schemas remain strict

Tool inputs are machine contracts and must be strict, server-owned, bounded, and validated.

Prohibited:

- browser -> provider direct calls;
- raw SQL/database access from the model;
- prompting "return JSON matching this schema" as the Analytics output contract;
- parsing final assistant prose to decide a financial mutation;
- regex/keyword/switch logic that tries to reconstruct model reasoning;
- exposing arbitrary canonical UUIDs when the server can bind the target itself.

### 8.3 Tool results are authoritative; prose is explanatory

Go/SQL owns:

- amounts;
- period boundaries;
- baselines;
- rankings;
- materiality;
- authorization;
- Wealth/cashflow reconciliation;
- drill-down targets.

The model may decide which available read-only tool to call and how to explain the returned facts.

If the model states a number in prose, that number must already exist in authoritative tool results. UI-critical numbers should still render from deterministic data rather than trusting copied prose.

### 8.4 Do not make Go the analyst/copywriter

Go must not replace the model with:

- keyword intent parsing;
- regex-based semantic understanding;
- switch-based analytical conclusions;
- canned narrative templates pretending to be AI analysis.

Go should expose facts and enforce invariants. The model should perform open-ended interpretation and conversational explanation.

### 8.5 Bounded tool loop

Analytics may require more than one read-only tool call to answer a useful question.

The orchestration must be bounded and side-effect safe.

For cycle analysis:

- read-only analytical tool calls may be chained within a bounded turn;
- financial mutations are not part of the analysis loop;
- a side-effecting action, if later introduced, must use an explicit tool and remain Go-authorized;
- the model must not gain arbitrary database exploration.

### 8.6 No recommendation contract

The current insight schema's mandatory recommendation paragraph is intentionally removed from the target product.

Analytics supports household discussion, not prescriptive advice.

Allowed natural response:

> "Groceries explains most of the increase against the recent baseline. The largest drivers were Pamella and Superindo. Was this a change in household needs that you expect to continue?"

Not allowed:

> "You should reduce groceries next month."

### 8.7 Skip AI when it adds no value

If deterministic materiality shows no meaningful candidate, the page can render the stable-cycle result without invoking a model merely to fill space.

### 8.8 Failure isolation

If an analytical tool fails, the gateway is unavailable, or the model cannot complete the analysis:

- deterministic Analytics still renders;
- charts and computed comparisons remain available;
- Go does not guess what the model would have said;
- there is no regex/template fallback masquerading as AI;
- there is no structured-JSON retry contract.

## 9. Explainability and supporting data

The deterministic page owns the inspectable evidence behind the review.

AI prose is supplemental.

For every material topic shown in the review, the UI should already be able to expose:

- current value;
- comparison baseline;
- delta;
- driver categories/merchants/transactions;
- data-quality context;
- deterministic drill-down.

The model may refer to these facts conversationally, but Richmod does not need to parse evidence references out of the prose to make the underlying review explainable.

"Why?" / "Supporting data" is driven by the deterministic analytical model, not by a JSON payload authored by the LLM.

## 10. Materiality policy

The exact policy belongs in Go and tests.

A candidate should not become material solely because of a large relative percentage on a tiny denominator.

At minimum consider:

- absolute IDR delta;
- relative delta;
- contribution to total cycle change;
- share of cycle spending;
- historical availability;
- transaction/merchant concentration;
- whether the comparison baseline is meaningful.

The materiality policy must have an explicit version.

Do not tune it silently in prompt text.

## 11. Baseline policy

Preferred order:

1. compare to previous completed cycle;
2. compare to previous 3-cycle median when at least enough comparable history exists;
3. optionally expose 6-cycle median as context;
4. never manufacture a baseline from incomplete/future periods.

The backend must expose baseline availability.

When history is insufficient, UI says so.

## 12. Meeting mode

Closed cycles expose:

~~~text
Review this cycle
~~~

Recommended flow:

1. Cycle outcome;
2. Spending shape;
3. Material changes;
4. Main drivers;
5. Savings and Wealth;
6. Loose ends/data quality;
7. Discussion points;
8. Household decisions.

This may be implemented as a focused state within `/analytics` rather than a separate route.

Requirements:

- keyboard usable;
- responsive;
- suitable for laptop/tablet;
- each step can drill down without losing selected cycle;
- progress is visual only unless persistence is explicitly needed;
- household decisions save explicitly.

Meeting mode remains fully useful when AI is disabled.

## 13. UI direction

Keep Richmod's calm, financial, data-first visual language.

Do not turn Analytics into:

- chatbot UI;
- neon AI dashboard;
- identical card grid;
- long wall of prose;
- generic fintech score screen.

Preferred hierarchy:

~~~text
period + review state
cycle outcome
primary chart
what changed
drivers
distribution
household
savings/wealth
data quality
discussion
decisions
~~~

Use whitespace and section composition so the page reads like a review document, not an admin console.

## 14. Drill-down behavior

Every material category/merchant/transaction finding should have a deterministic path to supporting data.

Prefer existing `/transactions` query-param filters where possible.

Examples:

~~~text
View transactions
View Groceries
View Pamella activity
Open Wealth snapshot
Open Review Inbox
~~~

The AI never generates arbitrary URLs or canonical IDs.

## 15. Data quality behavior

Current `data_completeness` may be reused but should not be presented as a magical AI certainty metric.

The revamp should expose concrete blockers.

Example:

~~~text
2 unresolved reviews
Rp750k confirmed expense uncategorized
RDN Wealth value last observed 34 days ago
~~~

A composite completeness value may remain operational metadata.

## 16. Persistence

Persisted AI commentary, if retained, may remain natural text because it is non-authoritative.

Do not add structured JSON persistence merely to make model prose machine-readable.

Whichever persistence design is chosen:

- no authoritative amount is stored only inside model prose;
- deterministic analytical facts remain separately queryable/auditable;
- native tool calls and their bounded results remain the factual interface;
- prompt/tool/policy version may be recorded for observability;
- household scope remains mandatory;
- final prose is never parsed to mutate financial state.

Schema changes require a new forward migration and update to `docs/DATABASE_SCHEMA.md`.

## 17. Telemetry

Telemetry must not store raw financial evidence or prose unnecessarily.

Useful bounded events include:

- cycle review opened;
- cycle review completed;
- finding drill-down opened;
- AI analysis generated/skipped/failed;
- no-material-finding;
- decision saved;
- deterministic fallback shown.

Model telemetry should record protocol/tool/policy/model/latency/status, not raw ledger rows.

## 18. Acceptance scenarios

### A — Stable cycle

Facts show no material deviation.

Expected:

- charts render;
- deterministic no-material-change state;
- generative model is not called;
- no filler paragraph.

### B — Previous cycle low, current normal

Current Dining is much higher than previous cycle but close to 3-cycle median.

Expected:

- UI shows both comparisons;
- finding does not sensationalize the previous-cycle percentage;
- discussion wording states current level is near recent baseline.

### C — Category drives most of the increase

Groceries contributes the majority of total expense delta.

Expected:

- category appears under What changed;
- driver merchants/transactions are visible;
- AI finding references only approved facts;
- numeric evidence is rendered deterministically;
- drill-down opens matching transactions.

### D — Large unusual-looking transaction

A large vehicle service transaction has no comparable recent transaction.

Expected:

- Richmod states the observable pattern;
- it does not automatically call it a one-off;
- household may record that interpretation/decision.

### E — Incomplete cycle

Open reviews materially affect analysis.

Expected:

- loose ends appear prominently;
- unsupported interpretation is suppressed;
- AI does not guess missing facts.

### F — AI gateway down

Expected:

- full deterministic review still works;
- charts, comparisons, savings, Wealth, and drill-down remain;
- no free-form fallback model request occurs.

### G — Household meeting decisions

Household records a decision.

Expected:

- explicit user action;
- separate from canonical transaction/Wealth facts;
- audited and household scoped;
- later cycles may display the previous decision without claiming it caused subsequent changes.

## 19. Definition of Done

- [ ] `/analytics` is useful as a closed-cycle household review.
- [ ] Existing useful charts remain and have clear analytical jobs.
- [ ] Current/previous/recent-baseline context is deterministic.
- [ ] Material changes and their drivers are computed server-side.
- [ ] Savings and Wealth are integrated into cycle review.
- [ ] Data-quality blockers are concrete.
- [ ] AI is optional and tool-first: financial data comes from native tools, while final explanatory prose may remain natural.
- [ ] No generative recommendation/advice field remains in the target contract.
- [ ] No material candidate can result in no AI call.
- [ ] Every numeric/financial claim can be traced to deterministic analytical data/tool results.
- [ ] Displayed amounts/percentages come from deterministic facts.
- [ ] AI failure leaves a complete deterministic experience.
- [ ] Closed-cycle meeting mode exists.
- [ ] Household decisions are explicit human-authored state.
- [ ] Desktop, tablet, and mobile remain usable.
- [ ] Relevant Go/web/worker tests cover deterministic and AI-disabled paths.
- [ ] Drift guard checklist passes.
