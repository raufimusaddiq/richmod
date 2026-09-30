# Analytics Cycle Review — Codex Sprint Plan

**Status:** implementation execution plan  
**Source contract:** `docs/RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md`  
**Business decision:** `docs/bdr/BDR-006-analytics-household-cycle-review.md`  
**Drift gate:** `docs/ANALYTICS_CYCLE_REVIEW_DRIFT_GUARD_CHECKLIST.md`

## 0. How Codex must execute this plan

Do not implement the entire revamp in one giant PR.

Each sprint below is a cohesive branch/worktree/PR from the latest `main` at the time work starts.

For every sprint:

1. read root `AGENTS.md`;
2. read the PRD, BDR, this plan, and drift guard;
3. inspect latest code instead of assuming this document matches old paths exactly;
4. create a dedicated linked worktree;
5. implement only that sprint's scope;
6. run the relevant verification;
7. inspect the final diff;
8. push the branch and open a PR;
9. do not claim the sprint complete until the drift guard items for that sprint pass.

Do not weaken an acceptance test because current implementation differs from this plan. The current explicit product contract wins over older Analytics docs.

## 1. Existing code to inspect first

At minimum:

~~~text
AGENTS.md

docs/RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md
docs/bdr/BDR-006-analytics-household-cycle-review.md
docs/ANALYTICS_CYCLE_REVIEW_DRIFT_GUARD_CHECKLIST.md
docs/adr/ADR-015-aggregate-only-llm-insights.md
docs/bdr/BDR-001-minimum-human-interaction-single-pass-intelligence.md

apps/api/internal/analytics/
apps/api/internal/insight/handler.go
apps/api/internal/wealth/
apps/api/internal/ledger/
apps/api/cmd/api/main.go

apps/worker/internal/insight/
apps/worker/internal/gateway/

apps/web/app/analytics/page.js
apps/web/app/components/Charts.js
apps/web/app/components/InsightCard.js
apps/web/app/lib/chartData.js
apps/web/app/lib/insightData.js
apps/web/app/globals.css

db/migrations/
docs/DATABASE_SCHEMA.md
scripts/check_native_only_llm.sh
~~~

Preserve useful current chart behavior unless the PRD explicitly replaces its analytical role.

---

# Sprint 1 — Deterministic Cycle Analysis Facts

## Goal

Build the server-owned analytical fact layer before changing AI prose or doing the full UI redesign.

## Required outcome

A selected salary cycle can produce one deterministic structured response containing the facts required by the PRD.

Suggested endpoint:

~~~http
GET /api/v1/analytics/cycle-review?cycle_start=YYYY-MM-DD
~~~

The exact endpoint name may change if a cleaner current API fit exists, but do not make the browser assemble authoritative analysis from five unrelated responses.

## Required facts

Implement server-owned computation for:

- cycle boundaries and state: active/closed;
- income;
- expense;
- net cashflow;
- savings allocated;
- unallocated residual;
- daily spending series;
- previous completed-cycle comparison;
- previous-three-completed-cycle median when enough history exists;
- category current/baseline/delta;
- contribution of each category to total expense change;
- top merchant drivers per category/change slice;
- top transaction drivers per category/change slice;
- member/shared attribution;
- Wealth current/previous snapshot context;
- Wealth change;
- confirmed-cashflow contribution;
- valuation/other residual;
- concrete data-quality blockers;
- objective change metrics suitable for tool-driven analysis.

## Baseline implementation

Do not use the browser to compute recent medians.

SQL/Go owns the baseline.

Tests must cover:

- no historical cycle;
- one prior cycle;
- enough cycles for 3-cycle median;
- previous cycle is an outlier but current matches median;
- zero/near-zero denominator;
- refund-adjusted expense semantics;
- transfers excluded;
- unresolved transactions excluded according to canonical Analytics policy.

## Analytical significance ownership

Do not add a Go "materiality engine" whose real job is to imitate an analyst.

Sprint 1 computes objective measurements only:

- absolute/relative deltas;
- recent baselines;
- contribution to total change;
- share of spending;
- merchant/transaction concentration;
- data-quality facts.

Those facts may be sorted and bounded for query efficiency.

Open-ended "what is noteworthy?" belongs to the intelligence layer in Sprint 2.
Use Jev only if a relevance question is genuinely bounded. Do not accumulate
thresholds, keywords, or switch cases in Go to decide what a human should discuss.

## Data quality

Return concrete blocker objects rather than only a score.

Example shape:

~~~json
{
  "kind": "OPEN_REVIEWS",
  "count": 2,
  "impact": "ANALYSIS_PARTIAL"
}
~~~

Do not invent stale-Wealth thresholds without documenting them in policy/tests.

## Code direction

Prefer new cohesive domain/service files under `apps/api/internal/analytics/` rather than growing one handler into a monolith.

HTTP handlers stay thin.

## Verification

Minimum:

~~~text
cd apps/api
go test ./...
go vet ./...
~~~

If new SQL integration behavior is added, use disposable PostgreSQL tests.

## Sprint 1 DoD

- [ ] one deterministic cycle-review API exists;
- [ ] browser does not need to derive authoritative deltas/medians;
- [ ] baseline availability is explicit;
- [ ] objective change metrics are available without semantic Go heuristics;
- [ ] no open-ended noteworthiness decision is hard-coded into Go;
- [ ] data-quality blockers are concrete;
- [ ] no AI changes are required to use the endpoint;
- [ ] drift guard sections A, B, C, D, and H pass.

---

# Sprint 2 — Tool-First Analytics Agent and Native Rendering

## Goal

Replace the current "supply aggregate JSON -> request structured narrative JSON"
pattern with a bounded tool-using analyst.

The model obtains financial facts through native Richmod tools, reasons over the
authoritative results, and returns natural prose through a native rendering tool.

## Architecture gate

Update ADR-015/ADR-030/ADR-033 as needed so the implementation matches the current
repo-wide native-tool contract.

## Required interaction model

~~~text
model phase
-> required provider-native tool call

READ tool
-> Go validates arguments
-> Go returns authoritative deterministic facts
-> next bounded model phase

RENDER tool
-> { message: "<natural prose>", supporting_refs?: [...] }
-> Go validates the envelope/references
-> Go displays message
-> message is never parsed back into finance state
~~~

Do not prompt the model to return JSON or a JSON-schema "analysis object."

## Analytics read tools

Expose a small semantic tool catalog over Sprint 1 facts. Candidate tools:

~~~text
get_cycle_overview
get_cycle_changes
get_category_drivers
get_merchant_drivers
get_supporting_transactions
get_savings_reconciliation
get_wealth_reconciliation
get_cycle_data_quality
~~~

The exact catalog may change after code inspection.

Rules:

- tools are read-only in this analysis loop;
- schemas are strict and server-owned;
- results contain authoritative facts, not pre-written narratives;
- no raw SQL or arbitrary DB exploration;
- no provider credentials;
- avoid model-facing canonical IDs where server-scoped refs work.

## Rendering tool

Provide one native display-only tool, e.g.:

~~~text
render_cycle_analysis
~~~

Minimal conceptual arguments:

~~~text
message: free-form string
supporting_refs: optional bounded list of server-issued fact refs
~~~

The `message` is intentionally natural language. Do not decompose prose into a
large DTO just so Go can reconstruct the response.

Go validates the envelope and supporting refs, then forwards the message. Go does
not inspect the wording to recover semantic state.

## Bounded multi-phase loop

Analytics may need dependent reads.

Extend/reuse the conversational native-tool orchestration so a turn can perform a
small number of bounded READ phases before RENDER.

Every LLM phase still returns provider-native tools. Raw final assistant text is
invalid in production.

Explicitly test:

- maximum phases;
- maximum read calls;
- unknown/unexposed tool rejection;
- no side-effect tool in Analytics analysis loop;
- final RENDER required for model-written prose.

## Semantic ownership

Go owns:

- calculations;
- exact period boundaries;
- authorization;
- data quality facts;
- tool execution;
- canonical state.

Generative intelligence owns:

- open-ended noteworthiness;
- synthesis across several returned facts;
- analytical explanation;
- natural meeting language.

Jev may own a genuinely bounded semantic relevance decision.

Go must not replace these with keyword parsing, regexes, arbitrary threshold
trees, switch-based narrative selection, or canned "AI" templates.

## Stable/no-noteworthy cycle

The model is allowed to use the rendering tool to say concisely that nothing
noteworthy stands out.

Do not require N findings.

Do not implement a Go semantic fallback whose purpose is to manufacture a stable
cycle conclusion.

## Persistence

If commentary is persisted:

- store the rendered message as non-authoritative text;
- optionally store validated supporting refs and tool/model/policy metadata;
- never parse the message later into finance state;
- keep deterministic analytical facts separately queryable;
- preserve old insight rows through an explicit compatibility path.

Do not introduce JSON model-output persistence just to make prose machine-readable.

## Native-only enforcement

Extend `scripts/check_native_only_llm.sh` or equivalent checks to catch:

- Structured/JSON-in-text LLM contracts;
- raw final model text in production conversational/Analytics paths;
- direct provider calls outside LiteRouter;
- Go regex/keyword semantic fallbacks introduced next to LLM paths where practical.

## Verification

Minimum:

~~~text
cd apps/worker
go test ./...
go vet ./...

cd apps/api
go test ./...
go vet ./...

scripts/check_native_only_llm.sh
~~~

## Required tests

- every model phase uses native tools;
- READ tool arguments are strictly decoded;
- unknown/unexposed READ tools fail;
- natural response is emitted through RENDER;
- raw final assistant text is rejected;
- RENDER message is not parsed into finance state;
- supporting refs, when supplied, must be server-issued/valid;
- dependent READ -> READ -> RENDER works within bounds;
- no side effect is available in the analysis loop;
- provider failure does not trigger regex/keyword/template analysis in Go;
- stable cycle can produce a concise no-noteworthy RENDER response;
- no recommendation/advice contract is required.

## Sprint 2 DoD

- [ ] financial data reaches the model only through approved native tools/context required by those tools;
- [ ] no "return this JSON schema" analysis contract remains;
- [ ] all LLM phases use provider-native tools;
- [ ] final natural prose uses a native rendering tool;
- [ ] Go does not parse final prose;
- [ ] open-ended analysis is not reimplemented as Go heuristics;
- [ ] AI failure is isolated from deterministic Analytics;
- [ ] native-only and anti-Go drift guards pass.

---

# Sprint 3 — Full /analytics Information Architecture Revamp

## Goal

Rebuild the Analytics page around cycle review while preserving useful charts.

## Primary UI

Desktop page order:

~~~text
Period / cycle selector
Cycle position
Spending shape
What changed
What drove the change
Where money went
Household view
Savings & Wealth
Data quality / loose ends
Discussion points
Household decisions
~~~

The page must not be a flat grid of equal cards.

## Cycle selection

Requirements:

- current cycle;
- recent closed cycles;
- explicit selected period;
- URL-addressable selection;
- selected cycle survives drill-down/back navigation when practical.

## Charts

Keep or adapt existing Recharts components.

Every chart must have:

- one defined question;
- deterministic values;
- readable tooltip;
- IDR formatting;
- empty state;
- mobile behavior;
- explanation/context adjacent to the chart.

Do not add a chart merely because data exists.

## What changed

Create a high-signal visual/table hybrid for objective category changes. The
model may explain which changes are actually noteworthy; the UI does not need a
Go-authored semantic label for every row.

Each item shows:

- current;
- comparison baseline;
- absolute delta;
- contextual relative delta when meaningful;
- contribution to total change;
- visual emphasis proportional to actual impact.

## Drivers

When a material category is selected/expanded:

- show merchant drivers;
- show top transaction drivers;
- link to `/transactions` with deterministic filters;
- preserve cycle context.

## Savings & Wealth

Do not duplicate the entire `/wealth` page.

Show only meeting-relevant summary:

- surplus;
- savings allocated;
- destinations;
- unallocated;
- net-worth movement;
- cashflow contribution;
- valuation/other difference;
- snapshot freshness.

Link to full Wealth detail.

## Data quality

Render concrete blockers with actions:

~~~text
2 reviews unresolved -> Open Inbox
Wealth snapshot stale -> Update Wealth
Category coverage incomplete -> View transactions
~~~

Do not present an AI-generated trust score.

## Discussion points

Render concise model-written analysis from the native rendering tool alongside
deterministic supporting data.

Do not require the model to produce a structured findings DTO.

The deterministic page owns "Why?" / supporting-data drill-down. If the rendering
tool includes server-issued supporting refs, use them only to focus that existing
deterministic UI.

If AI is unavailable:

- keep objective change metrics, charts, and supporting data;
- omit model-written interpretation/discussion wording;
- meeting remains coherent.

## UI language

Use Indonesian household-facing language consistent with current product.

Avoid:

- "AI says";
- "AI advice";
- "financial health score";
- "you overspent";
- blame/shame language;
- jargon-heavy model metadata.

## Verification

Minimum:

~~~text
cd apps/web
npm test
npm run build
~~~

Use synthetic fixture data for any screenshots/browser testing.

## Sprint 3 DoD

- [ ] page hierarchy matches PRD intent;
- [ ] useful existing charts preserved or deliberately replaced;
- [ ] every chart has an analytical job;
- [ ] What changed and Drivers are first-class;
- [ ] Savings/Wealth context appears;
- [ ] data-quality actions appear;
- [ ] native-rendered AI commentary appears alongside deterministic supporting evidence;
- [ ] AI-disabled state is complete;
- [ ] responsive and keyboard-usable;
- [ ] drift guard sections I and J pass.

---

# Sprint 4 — Closed-Cycle Meeting Mode + Household Decisions

## Goal

Make the selected closed cycle usable as an actual household review session.

## Meeting mode

Add a focused action:

~~~text
Review this cycle
~~~

Suggested sequence:

1. Outcome;
2. Spending shape;
3. Material changes;
4. Drivers;
5. Savings & Wealth;
6. Loose ends;
7. Discussion;
8. Decisions.

This can be an in-page focused mode; do not create a second product unless routing clearly benefits from it.

## Household decisions

Implement explicit human-authored cycle decisions.

Suggested conceptual entity:

~~~text
cycle_decision
- id
- household_id
- cycle_start
- created_by_user_id
- text
- status / active if needed
- created_at
- updated_at if editing is supported
~~~

Final schema design may differ.

Rules:

- household scoped;
- user authored;
- auditable;
- not a transaction;
- not an AI conclusion;
- no automatic financial mutation;
- no automatic causal claim in later cycles.

If schema changes, update `DATABASE_SCHEMA.md`.

## Previous decision context

A later cycle may show:

~~~text
Last cycle you recorded:
"Keep more cash liquid."
~~~

It may then show factual current-cycle evidence.

Do not generate:

~~~text
"Your decision succeeded."
~~~

unless the product has an explicit causal model, which is out of scope.

## Authorization

Use current household membership rules.

Do not create a separate household identity model.

## Verification

- API tests for cross-household access;
- decision create/read/update semantics if update exists;
- web tests for explicit save;
- meeting mode works with AI absent.

## Sprint 4 DoD

- [ ] one closed cycle can be reviewed end-to-end;
- [ ] household decisions save explicitly;
- [ ] decisions are separate from financial state;
- [ ] prior decisions can be displayed safely;
- [ ] no AI creates decisions;
- [ ] cross-household access fails closed;
- [ ] drift guard sections K and L pass.

---

# Sprint 5 — Hardening, Telemetry, Historical Cleanup

## Goal

Make the revamp maintainable and remove legacy drift traps.

## Documentation

After implementation:

- update README current Analytics description;
- update screenshots only with synthetic data;
- mark older Analytics insight docs historical/superseded where appropriate;
- update ADRs and schema docs to match actual final architecture;
- ensure docs describe current behavior, not planned behavior.

## Telemetry

Add only bounded privacy-safe product events that are useful.

Examples:

~~~text
CYCLE_REVIEW_OPENED
CYCLE_REVIEW_COMPLETED
ANALYSIS_RENDERED_NO_NOTEWORTHY_CHANGE
ANALYSIS_GENERATION_FAILED
FINDING_DRILLDOWN_OPENED
CYCLE_DECISION_SAVED
~~~

Do not log raw decision text in generic telemetry when the canonical decision record already owns it.

## Performance

Review:

- query count;
- repeated baseline calculations;
- payload size;
- web waterfall;
- AI call rate;
- cache/reuse semantics for closed cycles.

Closed-cycle deterministic facts should be stable unless historical canonical state is corrected.

Define invalidation behavior explicitly rather than assuming closed means immutable.

## Regression suite

Add a fixture cycle that covers:

- normal stable month;
- previous-cycle outlier;
- material category increase;
- large isolated transaction;
- incomplete review state;
- Wealth movement;
- AI available;
- AI unavailable.

## Final gates

Run all affected repository checks required by `docs/runbooks/sprint-delivery.md`.

Do not deploy unless explicitly requested and the production Environment approval flow is followed.

## Sprint 5 DoD

- [ ] docs reflect implementation;
- [ ] old conflicting docs are clearly historical;
- [ ] telemetry is privacy-safe;
- [ ] performance is acceptable;
- [ ] drift guard fully passes;
- [ ] full relevant CI passes.

---

# Suggested PR sequence

~~~text
PR A: deterministic cycle analysis facts
PR B: tool-first analytics agent + native rendering
PR C: /analytics full UI revamp
PR D: meeting mode + cycle decisions
PR E: hardening + telemetry + docs cleanup
~~~

Do not merge PR B before PR A's contract is stable.

Do not build PR C against a fake structured LLM DTO. Use the native READ/RENDER
contract from PR B and deterministic API fixtures for financial data.

Do not start PR D by adding goals/budgets/recurring features. Those are separate product initiatives.

---

# Codex completion report for each sprint

Report:

~~~text
branch
worktree
base SHA
commit(s)
tests run
test result
migration verification
drift guard result
PR URL
merge state
deployment state
known limitations
~~~

Do not claim a sprint is complete if the backend exists but the intended product surface is not wired, or if the UI renders values that are not backed by the deterministic contract.
