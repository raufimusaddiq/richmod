# ADR-015: Aggregate-only LLM insights

## Status

Accepted.

## Decision

Insight generation uses deterministic household analytics as its factual source.
Raw evidence, provider credentials, and unrestricted database access are never exposed
to the model. The model must obtain financial facts through server-owned native tools
or another explicitly approved bounded context supplied by the server.

The user-facing narrative is non-authoritative language. It may be natural free-form
prose because Richmod does not parse that prose back into canonical financial state.
Amounts, comparisons, materiality, authorization, and mutations remain deterministic.

Generation is limited to once per household period per hour. Completeness below
0.70 bypasses the gateway and returns a deterministic data-quality message. Insight
failures never alter financial state, and all requests/completions are audited.

## Tool-first amendment — 2026-09-30

When Analytics needs financial data, the model must use Richmod native read-only
tools with strict server-owned argument schemas and authoritative deterministic
results. Richmod must not ask the model to manufacture a JSON/structured-output
payload merely so Go can parse it.

After tool use, the model returns natural user-facing prose through a native
display/rendering tool. The rendering tool carries free-form language; it is not a
JSON-analysis DTO. The message is non-authoritative and must never be parsed into
ledger mutations, financial facts, analytical significance, or household decisions.

If a tool call fails, deterministic Analytics remains available. Do not replace the
failed tool with regex/keyword inference, narrative switch cases, canned analysis,
or by asking the model to guess missing financial data.

Go computes financial measurements. Open-ended "what is noteworthy?" and analytical
narrative remain intelligence responsibilities; they must not drift into deterministic
pseudo-NLP simply because Go owns the ledger.
