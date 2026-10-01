# Household cycle-review UI

Sprints 3–4 of [the execution plan](plans/analytics-cycle-review-sprint.md).
The source contract remains the [cycle-review PRD](RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md).

## Selection and data

- `/analytics?view=cycle&cycle=YYYY-MM-DD` selects one salary cycle.
  Omit `cycle` for the active cycle; the resolved salary start is written to
  the URL. A missing salary anchor displays the server's elapsed-calendar fallback
  and a settings action, without enabling commentary.
- `category=<category-id>` selects deterministic category evidence.
  `category=uncategorized` selects the API's empty-category group.
  Selection survives URL navigation, reload, and browser Back.
- `/analytics?view=calendar&range=3|6|12` retains secondary calendar analysis.
  `from=YYYY-MM&to=YYYY-MM` preserves the existing custom-month range contract.

Cycle review consumes one authoritative `cycle-review-v1` response. It does not
combine calendar aggregates or derive authoritative totals, baselines, averages,
shares, or significance in JavaScript. Browser math formats percentages and maps
server amounts to visual lengths only. Invalid/failed responses show a retry
state, never zero-valued financial facts or another cycle's stale result.

## Document hierarchy

### Compact presentation — October 1, 2026

The data hierarchy below is unchanged. Repeated section explanations now use
native `Tentang data ini` disclosures, available to keyboard and touch users.
Refund amount, total metrics, comparison cutoff/mode, missing-baseline states,
quality blockers and actions stay visible.
Comparison prose becomes a compact context strip (cycle, equal elapsed days vs
closed-cycle mode, median availability). Surplus and Wealth reconciliation
definitions remain available under named disclosures. No facts, calculations,
API fields, insight-generation rules or decision-saving behavior change.

Brand colours, chart outlines and self-hosted fonts follow
[Retro Ledger](brand-guidelines.md). The cycle chart retains its daily bars and
server-owned average; overview and wealth history use straight line segments,
not smoothed curves or inferred observations.

### Scan-first report — October 1, 2026

The default view is a report to scan, not prose to read. Net cashflow leads a
compact metric strip; daily spending and category changes sit side-by-side on
wide screens, stack on smaller screens. Previous/current/median expense amounts
use directly labelled bars on one zero-based magnitude scale. Signed values stay
explicit; null history stays missing. Browser arithmetic only sizes visual bars.

Every category remains in the compact current/delta list in server order.
The full comparison table retains previous/median values, both absolute/relative
deltas and signed contribution under **Perbandingan lengkap**. Selection opens
and focuses category evidence without a second API call. Merchant/transaction
tables remain household/cycle-bound and keyboard-scrollable.

Distribution, attribution, optional commentary and human-authored decision
context use native disclosures. Decisions open automatically in their meeting
step or when a draft exists; drafts/save/revoke behavior is unchanged. Savings
destinations and net-worth movement stay visible; detailed balances, cashflow
contribution, valuation/other difference and snapshot provenance are expandable.
Unavailable reconciliation remains visibly unavailable.

**Buka semua detail** exposes all tables, metadata and explanations in one
explicit action. No fact is removed, abbreviated, synthesized or recomputed.
Concrete quality blockers/actions stay outside disclosures. Focused meeting
mode still opens its active section and uses the same eight steps. Calendar
analysis, AI generation and all financial/data mutation behavior are unchanged.

1. Explicit period, state, measured cutoff and Jakarta timezone.
2. Cycle outcome: dominant net cashflow, income, refund-adjusted expense,
   savings allocated, unallocated surplus.
3. Daily spending shape: retained daily bars and server-owned average reference,
   peak, peak share, zero-spend days, plus accessible daily values.
4. Category changes: current, previous, median of three eligible cycles,
   signed absolute/relative deltas against both baselines, contribution to total
   change, proportional visual weight. No significance labels.
5. Selected-category drivers: bounded merchant comparisons, supporting
   expense/refund transactions, category share and count.
6. Category distribution and whole-cycle merchant drivers.
7. Descriptive household attribution, never member responsibility rankings.
8. Savings destinations and Wealth observations/reconciliation. Snapshot links,
   timestamps and server observation ages remain explicit. Missing reconciliation
   stays missing; no guessed cycle-end balance, investment P/L or stale threshold.
9. Concrete quality blockers and Inbox, transaction, settings or Wealth actions.
10. Optional non-authoritative cycle commentary alongside supporting-data links.

11. Human-authored decisions for the selected closed cycle, plus the immediate
    previous completed cycle's decisions as descriptive context. Decisions are
    not AI output or evidence of a causal effect on current finances.

## Focused meeting mode

Closed salary cycles expose **Tinjau siklus ini**. The same page/sections form
eight steps: position, spending shape, changes, drivers, savings/Wealth, loose
ends, discussion, decisions. The `review` query parameter stores the active step
(`position`, `spending-shape`, `changes`, `drivers`, `savings-wealth`, `quality`,
`discussion`, `decisions`). Invalid steps are ignored. Meeting controls only
activate when authoritative period state is `SALARY_CYCLE` + `CLOSED`.

The active heading receives keyboard focus. Native step buttons, Back/Next,
and a return-to-full-review action remain available. Progress is presentation
state, not a persisted completion or financial claim. Choosing a category in
the Changes step opens Drivers; transaction links and the ledger's return link
retain `cycle`, category, and `review`. Other navigation can use browser Back.
Category/household distribution remains in the full review, not duplicated into
the focused sequence. Charts and evidence remain unchanged; model unavailability
does not block any meeting step or decision save.

## Explicit household decisions

`GET /api/v1/analytics/cycle-decisions?cycle_start=YYYY-MM-DD` returns selected
cycle notes, its server-resolved immediate previous completed cycle's notes,
and `previousCycleStart`. It never accepts a household or author override.
Active cycles can read prior context but cannot create new decisions.

`POST /api/v1/analytics/cycle-decisions` accepts only `cycleStart` and `body`.
The service validates the exact date, confirms a closed salary cycle through
the shared facts engine, trims text, rejects empty/NUL/over-2000-character
text, and rechecks active membership at the write boundary. Payloads are limited
to 16 KiB; unknown fields and trailing JSON are rejected.

Any active household member can save or explicitly revoke a shared decision:
`POST /api/v1/analytics/cycle-decisions/{id}/revoke`. Wrong-household IDs return
404. Revocation is an audited soft delete; a repeated revoke returns 404 with
no second mutation/audit. Original text and authorship remain in PostgreSQL.
Notes cannot be edited in place: revoke an old note and save a replacement.

Migration `00074_cycle_decision.sql` introduces the separate household-scoped
entity, author, cycle start and timestamps. Creation/revocation commit with the
existing `audit_log` in the same transaction. No decision text is copied into
generic telemetry or audit payload JSON. No AI tool can create/revoke decisions.
Transactions, balances and Wealth observations are never changed by notes.

Saving requires the named button or Ctrl/Cmd+Enter. Nothing auto-saves; typing
never requests a model turn. Success waits for the server; failures keep the
draft and instruct users to check the list before retrying an uncertain save.
Drafts stay in page memory per cycle, survive step/selector changes, and warn
before leaving/reloading; there is no browser-storage or background draft write.

## Drill-down

Transaction links carry `from`, inclusive `to`, `status=CONFIRMED`,
`type=SPENDING`, cycle start, and applicable category/merchant/transaction ID.
The UI converts the facts API's exclusive cutoff to the ledger's inclusive date
filter, so the next cycle's Jakarta midnight is excluded.

The household-scoped ledger list adds:

- `type=SPENDING`: confirmed status is a separate filter; type selects
  `EXPENSE` and `REFUND`, never income or transfers.
- `merchantId=<uuid>`: validated exact canonical merchant binding, not a
  substring name search.
- `categoryId=uncategorized`: null category binding.
- `id=<uuid>`: exact household-scoped evidence transaction binding, also used
  by the existing detail drawer.

Existing transaction filters, pagination and authorization remain intact.
The ledger preserves the cycle context and offers a return link; merchant
filters can be explicitly cleared. All of this is read-only.

## Commentary isolation

Only non-historical commentary matching the selected salary-cycle start is shown.
Both `CURRENT_CYCLE` and `SALARY_CYCLE` metadata are supported.
Historical advice rows remain unchanged in PostgreSQL but are not presented as
current cycle-review commentary.

Generation requires an explicit user action and passes the selected start to
the existing tool-first endpoint. Loading the review or switching categories
does not request a model turn. Provider failure leaves charts, comparisons,
drivers, savings, Wealth and quality actions intact. No confidence/quality score,
recommendation DTO or prose-to-finance parsing is added. The existing server
completeness gate remains authoritative.

## Telemetry and invalidation

Bounded product events are emitted through structured logs, never a new
schema: `CYCLE_REVIEW_OPENED` (one line per successful facts load, with
`version`, `period_kind`, `period_state`) and `CYCLE_DECISION_SAVED` (one line
per committed human-authored decision). Amounts, dates, household/decision IDs,
names and decision text are absent by construction; the canonical
`cycle_decision` and `transaction` rows remain the only owners of that data. No
event is emitted for opening the page without a confirmed facts load or for
`ANALYSIS_RENDERED_NO_NOTEWORTHY_CHANGE`, which lives in model text and would be
duplicated or unparseable. See `docs/ANALYTICS_CYCLE_REVIEW_TELEMETRY.md`.

Closed cycles are **not** assumed immutable. `/api/v1/analytics/cycle-review` and
`/api/v1/insights` set `private, no-store`, and the web fetch sends
`cache: no-store`, so a corrected historical transaction or re-confirmed review
is reflected on the next load instead of serving stale closed-cycle facts. The
insight list is now filtered by `cycle_start` before its `LIMIT`, so older
selected cycles remain reachable once commentary exists for newer cycles, and
the audit snapshot/transcript are stripped from the presentation payload.

Persisted commentary is dated snapshot-based text, not automatically regenerated
by HTTP no-store. Its existing one-hour generation reuse remains unchanged;
canonical corrections update current facts on reload, not past model prose.

## Verification

- `npm test`: formatting/null baselines, date boundaries, URL selection,
  historical commentary exclusion, deterministic rendering boundaries.
- API integration tests: exact merchant matching, refunds, inclusive cutoff
  translation, uncategorized spending, household isolation.
- CI web build and `scripts/analytics-review-smoke.mjs`: synthetic active and
  closed cycles, median/outlier context, keyboard category selection, correct
  drill-down filters, browser Back, calendar URL state, provider outage, failed
  facts, missing salary anchor, no automatic generation, no page overflow.
- Screenshots use only synthetic fixtures at 1440, 1024, 390 and 320px widths;
  CI publishes them as `cycle-review-screenshots` for visual inspection.

Sprint 4 verification adds malformed/oversized/unknown-field input cases,
closed-cycle validation, prior context, author/audit binding, cross-household
read/revoke denial, inactive-member write denial, retained soft revocation,
and no financial-row creation. Synthetic browser checks cover all eight steps
without AI, keyboard save, failed-save draft retention, explicit retry/revoke,
meeting drill-down/return, no automatic POST and all four viewport widths.

No provider change, deployment, restart or production DML. Migration verification
uses a disposable PostgreSQL database; production application follows the runbook
only after a separately requested and approved deployment.
