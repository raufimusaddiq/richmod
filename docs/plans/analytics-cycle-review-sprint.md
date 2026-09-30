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
- top merchant drivers per material category;
- top transaction drivers per material category;
- member/shared attribution;
- Wealth current/previous snapshot context;
- Wealth change;
- confirmed-cashflow contribution;
- valuation/other residual;
- concrete data-quality blockers;
- versioned materiality candidates.

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

## Materiality

Introduce a versioned policy, for example:

~~~text
cycle-materiality-v1
~~~

Do not encode significance rules only in prompts.

The first implementation may use a simple explicit rule set, but it must combine absolute impact with contextual change so tiny values do not become "important" from percentage alone.

Return why a candidate qualified using bounded reason codes, for example:

~~~text
LARGE_ABSOLUTE_DELTA
HIGH_CONTRIBUTION_TO_TOTAL_CHANGE
HIGH_SHARE_OF_CYCLE
NOVEL_LARGE_TRANSACTION
CONCENTRATED_DRIVER
~~~

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
- [ ] materiality policy is versioned;
- [ ] material candidate reason codes exist;
- [ ] data-quality blockers are concrete;
- [ ] no AI changes are required to use the endpoint;
- [ ] drift guard sections A, B, C, D, and H pass.

---

# Sprint 2 — Structured Native-Tool Analysis Contract

## Goal

Replace generic narrative-generation behavior with structured, evidence-bound cycle findings.

## Architecture gate

Before materially changing the existing ADR-015 behavior, update or supersede the relevant ADR in the same PR if required by the final implementation.

Do not bypass the architecture-document requirement.

## Native-tool rule

Generative Analytics must use:

~~~text
LiteRouter
-> existing gateway
-> NativeToolCall
-> Required: true
-> strict server-owned schema
~~~

No free-form fallback.

## Fact packet

Create an approved AI packet from Sprint 1 facts.

It must contain:

- semantic fact refs;
- material candidates;
- bounded comparison metadata;
- data-quality state.

It must not contain:

- raw transaction rows;
- raw evidence;
- source payloads;
- canonical transaction/account UUIDs;
- email addresses;
- Telegram messages;
- household secrets.

Driver facts may be aggregated and labeled with safe display names where already suitable for household presentation.

## Output tool

Implement a strict tool such as:

~~~text
write_cycle_analysis
~~~

Required output fields:

~~~text
headline
findings[]
no_material_finding
data_quality_note
~~~

Each finding:

~~~text
kind
title
interpretation
evidence_refs[]
discussion_question
~~~

No `recommendation` field.

## Validation

Worker validation must reject:

- wrong tool name;
- missing required tool call;
- unknown fields;
- invalid enum;
- too many findings;
- text beyond limits;
- evidence ref not present in packet;
- finding with no evidence;
- duplicate unsupported refs;
- malformed output.

If output validation fails:

~~~text
deterministic Analytics survives
AI analysis state = failed/unavailable
no free-form retry
~~~

## No-material path

If Sprint 1 materiality yields no useful candidates:

- do not call the generative model;
- persist/return deterministic no-material state;
- record that generation was skipped.

## Jev

If current bounded verifier is useful for a genuinely bounded selection question, it may remain only when it answers a distinct residual/selection question.

Do not use Jev and the generative model to repeat the same "is this important?" decision.

Follow BDR-001 semantic ownership.

## Persistence

Prefer structured persisted output.

If schema changes:

- add forward-only migration;
- update `docs/DATABASE_SCHEMA.md`;
- preserve existing insight rows;
- define compatibility/read behavior for old rows;
- do not rewrite historical output silently.

## Native-only enforcement

Extend `scripts/check_native_only_llm.sh` or equivalent repository checks so Analytics worker/API generative paths cannot regress to direct/free-form LLM calls.

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

Apply any migration to disposable PostgreSQL.

## Required tests

- native tool is required;
- correct tool name accepted;
- prose outside/without tool call rejected by gateway/contract;
- unsupported evidence ref rejected;
- empty evidence rejected;
- no material candidate skips generative call;
- AI gateway failure leaves deterministic analysis intact;
- old persisted insight compatibility path behaves explicitly;
- no recommendation field exists in new structured contract.

## Sprint 2 DoD

- [ ] AI contract is structured;
- [ ] all AI findings are evidence-bound;
- [ ] no free-form Analytics model output path exists;
- [ ] no material finding skips model generation;
- [ ] AI failure is isolated;
- [ ] persisted schema/audit state is valid;
- [ ] native-only guard passes;
- [ ] drift guard sections E, F, G, and H pass.

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

Create a high-signal visual/table hybrid for material categories.

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

Render structured findings, not one large paragraph.

Each finding includes deterministic evidence and a "Why?" or supporting-data affordance.

If AI is unavailable:

- render deterministic material candidates;
- omit generated interpretation/discussion wording;
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
- [ ] structured AI findings render with supporting evidence;
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
ANALYSIS_GENERATION_SKIPPED_NO_MATERIAL_SIGNAL
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
PR B: structured native-tool cycle analysis
PR C: /analytics full UI revamp
PR D: meeting mode + cycle decisions
PR E: hardening + telemetry + docs cleanup
~~~

Do not merge PR B before PR A's contract is stable.

Do not build PR C against mock AI prose. Use the structured contract from PR B or a typed fixture that exactly mirrors it.

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
