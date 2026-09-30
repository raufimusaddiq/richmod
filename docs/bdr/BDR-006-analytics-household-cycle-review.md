# BDR-006: Analytics as the Household Cycle Review

## Record type

Business Decision Record.

## Status

Proposed product decision — 2026-09-30.

## Decision owner

Product Design.

## Related documents

- `docs/RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md`
- `docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md`
- `docs/bdr/BDR-001-minimum-human-interaction-single-pass-intelligence.md`
- `docs/adr/ADR-015-aggregate-only-llm-insights.md`
- `docs/INTELLIGENCE_ROUTING_DRIFT_GUARD_CHECKLIST.md`

## Business problem

Richmod has become effective at turning evidence into trusted household financial records, but the value after capture is still under-realized.

The current Analytics surface exposes correct charts, rankings, and a generated narrative. That is useful for inspection, but it is not yet the product a household can rely on when a salary cycle ends and two members want to review their finances together.

The failure mode is subtle:

~~~text
good capture
+ correct ledger
+ many charts
+ generic AI summary
!= useful household financial review
~~~

The household should not have to translate dashboards into its own review process every month.

## Product decision

Richmod Analytics will be designed primarily as a **household cycle review**, not as a generic dashboard and not as an AI advice surface.

For a completed salary cycle, Analytics must help the household answer:

1. What happened?
2. What materially changed?
3. What drove the change?
4. Is the change actually unusual against recent history?
5. Where did the surplus go?
6. How did Wealth change?
7. Is the data complete enough to trust the review?
8. What is worth discussing?
9. What does the household itself want to remember or decide?

Charts remain a core part of the product. The decision is not "replace charts with AI." The decision is:

> Charts show the facts. Deterministic analysis explains the movement. Optional AI makes the already-supported findings easier to discuss.

## Product hierarchy

The accepted hierarchy is:

~~~text
trusted household state
-> deterministic cycle analysis
-> deterministic visual explanation
-> optional evidence-bound AI interpretation
-> household discussion
-> household-authored decisions
~~~

The reverse hierarchy is rejected.

In particular, the product must not become:

~~~text
LLM opinion
-> chart decoration
-> household follows recommendation
~~~

## Native-tool AI decision

Generative AI in Analytics follows a **tool-first** model.

The business rule is:

> If the model needs financial data or state, give it a native Richmod tool. Do not ask it to manufacture structured JSON for Go to parse.

Native tools are the machine interface. Their arguments are strict and server-owned, and their results come from deterministic Go/SQL logic.

The final household-facing explanation remains natural free-form prose, but it is
carried inside a native rendering/respond tool. Go forwards the message and never
parses it into ledger mutations, analytical significance, arithmetic, or household
decisions.

This preserves the correct separation:

~~~text
Go / SQL
= facts, arithmetic, authorization, state

native tools
= safe data/action interface

LLM
= open-ended reasoning + conversational explanation
~~~

The following is explicitly rejected:

~~~text
prompt model to return JSON
-> unmarshal JSON in Go
-> treat fields as the product contract
~~~

Also rejected:

~~~text
avoid LLM reasoning
-> encode semantic understanding in Go regex/keywords/switch templates
~~~

If a native data tool or the model fails, Analytics degrades to its deterministic charts and facts. Go does not imitate the missing analysis with canned "AI" prose.

## No-advice decision

The target output is **discussion support**, not recommendation.

The previous generic insight contract required a recommendation paragraph. That is no longer the desired product behavior for the cycle-review experience.

Accepted:

> "Groceries contributed most of the increase against the recent baseline. Was this a change in household needs that you expect to continue?"

Rejected:

> "Reduce grocery spending by 15% next month."

The household decides. Richmod provides evidence and context.

## Analytical significance decision

Richmod should not surface every change.

A small percentage movement on a small amount does not deserve a meeting topic merely because it exists.

Go owns the measurements that make significance assessable: amounts, deltas,
baselines, shares, contribution, concentration, and data quality.

Open-ended "what is actually worth discussing?" is an intelligence responsibility,
not a growing Go threshold/switch system. Generative intelligence may judge
noteworthiness from authoritative native-tool results. Jev may own a relevance
decision only when the question is genuinely bounded.

This prevents two opposite failures: AI filler on one side and hard-coded
pseudo-intelligence in Go on the other.

## Baseline decision

The immediately previous cycle is useful but insufficient.

Where history permits, Analytics must provide recent-baseline context, especially a previous-three-completed-cycle median.

This prevents false drama such as:

~~~text
Dining +75% vs previous cycle
~~~

when the current amount is actually normal against recent history.

The product prefers a truthful nuanced statement over a more dramatic percentage.

## "No insight" decision

No material finding is a successful product result.

If a cycle is stable, Richmod should say that concisely or simply show deterministic context.

The model may return a concise "nothing noteworthy" analysis through the native
rendering tool. The application must not force a fixed number of observations
solely because an AI card exists.

This is a deliberate anti-slop policy.

## Explainability decision

Any AI-assisted finding must support a household-level "Why?" action.

That expansion shows deterministic evidence:

- current value;
- baseline;
- delta;
- driver categories/merchants/transactions;
- data-quality context;
- links to the relevant Richmod records.

The model does not generate drill-down URLs.

A finding that cannot be explained this way should not be shown.

## Cross-channel analytical reuse

The analytical engine is a Richmod capability, not a page-local feature.

The same deterministic calculations and analytical READ tools used by `/analytics`
should be reusable by the existing Telegram conversational agent.

A Telegram question such as:

> "Kenapa pengeluaran cycle ini naik?"

should cause the conversational model to retrieve the same authoritative cycle
facts/drivers that support the Web review, then explain them naturally.

This BDR does not change Telegram's conversational response protocol. ADR-033
remains authoritative for that channel.

Rejected:

~~~text
Web has one analysis service
Telegram has separate Go rules / SQL / thresholds for the same question
~~~

Accepted:

~~~text
shared analytical facts + tools
-> Web presentation
-> Telegram conversational presentation
~~~

## Household-member decision

Member attribution is useful for understanding who initiated a transaction, but Analytics must not turn attribution into household scoring.

Rejected product behavior includes:

- "best spender";
- "worst spender";
- responsibility scores;
- behavioral rankings;
- shaming language;
- AI judgments about one member's habits.

Member data is descriptive and household-scoped.

## Household decisions

A cycle review should be able to end with explicit household-authored notes or decisions.

These decisions are a separate product concept from transactions, categories, reviews, and Wealth observations.

They may be shown in future cycle reviews as context.

Richmod must not infer that a later financial change was caused by a prior decision unless that causal relationship is actually established.

## Alternatives considered

### Alternative A — Keep current charts and polish the AI paragraph

Rejected.

This improves appearance but not product utility. The underlying insight input remains too shallow and the output remains difficult to verify.

### Alternative B — Replace Analytics with an AI chat

Rejected.

It hides the deterministic financial model, weakens repeatability, and turns a household review into prompt-writing.

### Alternative C — Let the LLM analyze raw transactions

Rejected.

It violates Richmod's privacy and authority model, makes arithmetic/relevance probabilistic, and makes claims harder to audit.

### Alternative D — Deterministic charts only, no AI

Viable as a safe fallback but not the full target.

Charts and deterministic analysis must stand alone, but native-tool AI can add value by turning material findings into concise human discussion language.

### Alternative E — Deterministic facts + native-tool analytical reasoning

Accepted.

Go/SQL expose authoritative finance facts and calculations through native tools.
The model performs open-ended analytical reasoning and natural explanation, then
emits that prose through a native rendering tool. This preserves financial
authority without turning Go into the analyst.

## Success criteria

This decision is successful when a household can open one completed cycle and, without manually constructing its own spreadsheet review:

- understand the financial outcome;
- identify the few changes that actually mattered;
- inspect why they mattered;
- distinguish noise from a recent baseline;
- understand savings and Wealth movement;
- see unresolved data-quality blockers;
- discuss neutral evidence-backed prompts;
- record its own conclusions.

AI availability is not a prerequisite for completing that review.

## Product invariants

~~~text
charts remain deterministic
+
numbers remain deterministic
+
semantic significance remains intelligence-owned
+
all LLM phases use native tools
+
final prose uses a native rendering tool
+
analysis is grounded in authoritative tool results
+
household decisions remain human
~~~

not:

~~~text
more AI text
=
better Analytics
~~~

## Anti-Go semantic boundary

This decision explicitly rejects Go as a substitute intelligence layer.

Go may implement exact financial rules and deterministic calculations. It must
not implement open-ended semantic understanding with keyword matching, regexes,
narrative switch cases, canned "insight" templates, or arbitrary thresholds
whose real purpose is to decide what a human would find noteworthy.

If a new Go branch needs to understand a human sentence, decide what is worth
discussing, or write analytical narrative, the implementer must stop and verify
whether Jev or generative intelligence owns that responsibility.

A model/provider outage does not transfer semantic ownership to Go. Deterministic
product functionality may remain available; semantic analysis may be unavailable.

## Revisit triggers

Revisit this BDR if:

- households consistently use a different period than salary cycle for reviews;
- the chosen intelligence routing for noteworthiness suppresses genuinely useful findings;
- evidence-bound AI adds no measurable utility over deterministic explanations;
- the Wealth/cashflow model changes materially;
- the product intentionally expands into advice, which would require a separate explicit product and safety decision.

Any revisit must preserve Richmod's financial correctness and explainability constraints.
