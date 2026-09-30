# Analytics cycle-review telemetry and invalidation

Sprint 5 ([execution plan](plans/analytics-cycle-review-sprint.md)) hardening
record. The source contract is the [cycle-review PRD](RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md),
sections 16-17.

## Events

| Event | Where | Fields | Deliberately absent |
| --- | --- | --- | --- |
| `CYCLE_REVIEW_OPENED` | `CycleReview` after a successful facts load | `version`, `period_kind`, `period_state` | household/decision IDs, amounts, dates, names, evidence, decision text |
| `CYCLE_DECISION_SAVED` | `CreateDecision` after the service commits | none | every field; canonical text/authorship stay in `cycle_decision` |

Both are structured `slog` logs. Telemetry uses the existing logging pipeline,
so it adds no table, migration, endpoint or write path to the financial
transaction. Opens count successful API loads, including reloads/retries,
not unique visitors or confirmed browser rendering. Logs are best-effort,
not durable exactly-once product accounting; a process crash can lose an event.

Not emitted, with reasons:

- `ANALYSIS_RENDERED_NO_NOTEWORTHY_CHANGE` — the model's prose result; detecting
  it in Go would be regex/keyword drift (drift guard H).
- `ANALYSIS_GENERATION_FAILED` — derivable from the existing `insight.status`.
- `FINDING_DRILLDOWN_OPENED` / `CYCLE_REVIEW_COMPLETED` — no server events today;
  add to the same logging path if the product questions appear.

## Performance

- `cycle-review-v1` already computes all section facts in one read-only
  repeatable-read transaction with a bounded number of statements; per-statistics
  cycle measures are loaded once, so baselines are not recalculated per row.
- The insight list no longer ships `input_metrics_json->'facts_snapshot'` or
  `tool_reads`, and it filters by `cycle_start` before `LIMIT 12`.
- AI rate is unchanged: generation stays explicit and rate-limited by the
  existing hourly cache in `handler.go`.

## Invalidation

Closed does not mean immutable. Corrected transactions, salary anchors, category
bindings, savings destinations, reviews and Wealth observations affect historical
facts. Every facts request recomputes them in a fresh database snapshot; browser
and HTTP reads are `private, no-store`. Category/meeting-step selection reuses the
current page's facts without refetch; reload after external canonical corrections.

Persisted commentary remains a dated, non-authoritative observation of its
recorded facts/transcript. HTTP no-store does not regenerate commentary or erase
its audit snapshot. Explicit generation reuses pending/current-version successful
jobs for one hour, then allows a new analysis. Canonical corrections within that
hour change facts immediately but do not bypass the rate limit; the UI shows the
commentary timestamp and supporting current facts. No timeless accuracy claim is
made for old prose, and no model interpretation is used as invalidation logic.
