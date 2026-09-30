# ADR-015: Tool-first cycle commentary

## Status

Accepted.

## Decision

Insight generation uses deterministic household analytics as its factual source.
Raw evidence, provider credentials, and unrestricted database access are never exposed
to the model. The model must obtain financial facts through server-owned native tools
through shared channel-neutral analytical READ tools. Preloaded aggregate
narrative DTOs are retired for new Analytics commentary.

The user-facing narrative is non-authoritative language. It may be natural free-form
prose because Richmod does not parse that prose back into canonical financial state.
Amounts, comparisons, objective change measurements, authorization, and mutations
remain deterministic. Open-ended noteworthiness and prose are model-owned.

Generation is limited to once per household period per hour. Completeness below
0.70 bypasses the gateway and marks commentary unavailable; deterministic
quality blockers remain available through the facts API. Insight
failures never alter financial state, and all requests/completions are audited.

## Tool-first amendment — 2026-09-30

When an Analytics model needs financial data, it must use Richmod native read-only
analytical tools with strict server-owned argument schemas and authoritative
deterministic results. Those READ tools should also be reusable by the existing
Telegram conversational agent for analytical questions.

Richmod must not ask the model to manufacture a JSON/structured-output payload
merely so Go can parse it.

For the Web Analytics AI surface, user-facing prose is non-authoritative and must
never be parsed into ledger mutations, financial facts, analytical significance,
or household decisions.

Telegram reuse follows ADR-033's existing conversational response contract. This
Analytics ADR does not redefine Telegram's final-response protocol.

If a tool call fails, deterministic Analytics remains available. Do not replace the
failed tool with regex/keyword inference, narrative switch cases, canned analysis,
or by asking the model to guess missing financial data.

The implementation lives in `apps/reviewdomain/analyticscore`: one repeatable-read
fact engine, strict native schemas, request-local references, no financial write
capability, and explicit projections excluding canonical identifiers and raw
evidence. Both the API and Telegram consume it.

Web commentary uses at most 5 model phases, 5 READs per response, 8 READs per
turn, 8 seconds per model invocation, and 45 seconds overall. The rendering
tool is exposed only after cycle overview and data-quality READs completed.
It carries one natural `message`, never a findings/recommendation DTO. Rendering
cannot share a batch with READs. Unknown/malformed tools fail before any READ
in that batch executes. Gateway failure emits no substitute analytical prose.

New jobs use `cycle-analyst-v3`. The server stores request facts and executed
READ transcript as audit evidence, not as initial model context. Completed
older rows remain untouched and are explicitly marked historical by the list
API. Pending legacy jobs fail with `superseded_contract` instead of regenerating
the old advice contract. No schema migration is required.

Completeness uses categorized gross confirmed expense / gross confirmed expense
(refunds do not inflate coverage), reduced by the existing 0.90 factor when
reviews remain. Zero spending with reviews has 0.50 coverage; zero spending
without reviews has full coverage. The 0.70 gate concerns supported data, never
whether a change is worth discussing. Concrete blockers remain authoritative.

Go computes financial measurements. Open-ended "what is noteworthy?" and analytical
narrative remain intelligence responsibilities; they must not drift into deterministic
pseudo-NLP simply because Go owns the ledger.

## Hardening amendment — 2026-09-30

`GET /api/v1/insights?cycle_start=YYYY-MM-DD` filters the household's commentary
before the existing 12-row limit; omit the selector for the legacy recent list.
Malformed dates fail before a database read. The presentation payload omits
`facts_snapshot` and `tool_reads`; their PostgreSQL audit values are preserved.
Facts and commentary HTTP reads are `private, no-store`. Persisted commentary
still describes its timestamped snapshot, not newly corrected historical facts;
the existing explicit generation/hourly reuse policy is unchanged.

Cycle-review usage emits bounded, non-identifying log events, not financial
writes or new persistence. Definitions, privacy boundaries, query budget and
invalidation are recorded in
[the hardening note](../ANALYTICS_CYCLE_REVIEW_TELEMETRY.md).
