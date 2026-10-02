# Household cycle-review UI

Sprints 3–4 of [the execution plan](plans/analytics-cycle-review-sprint.md).
The source contract remains the [cycle-review PRD](RICHMOD_ANALYTICS_CYCLE_REVIEW_PRD.md).

> **Current page (2026-10-02):** the first section is the cycle-to-cycle ledger
> ([sprint plan](plans/analytics-cycle-ledger-sprint.md)). Sections under
> "Document hierarchy" below are dated records of earlier refinements; where one
> disagrees with "Cycle ledger" or "Page order", those two win.

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

## UX audit fixes — October 2, 2026

- Choosing the selected category again clears it (the ledger matrix and the change
  list alike), and the evidence panel has **Kembali ke ringkasan siklus** back to the
  ledger when not in a meeting.
- **Buka semua detail** is a toggle ("Tutup semua detail"); a new cycle starts collapsed.
- The generated commentary is "pembahasan" everywhere (section, card, button, states);
  the card says up front that it is limited to one per hour.
- Each section explainer names its subject ("Tentang posisi siklus", "Tentang pola
  pengeluaran", …) instead of ten identical "Tentang data ini" disclosures.

## Code layout

`app/analytics/page.js` owns URL selection, data loading (facts, commentary, the
unsaved-draft guard) and composition. The pieces live beside it: `shared.js`
(`SectionTitle`, `Metric`), `CycleSections.js` (position, comparison, changes,
drivers, savings and Wealth, quality, meeting navigation) and `CalendarReview.js`
(the calendar view). The ledger and pace chart are in `app/components`
(`CycleLedger.js`, `Charts.js`) with their helpers in `app/lib/cycleLedger.js`.
Tests read the whole directory with `tree("app/analytics")`.

## Cycle ledger — October 2, 2026

The first section of the cycle view is a **cycle ledger**
([plan](plans/analytics-cycle-ledger-sprint.md), [mock](assets/analytics-cycle-ledger-mock.html)).
It renders the API's `history[]` (see the [facts API](ANALYTICS_CYCLE_REVIEW_API.md)):

- one column per salary cycle, oldest first, with income and net-expense bars and
  direct value labels in Rp juta, net cashflow, and the server-computed
  `expenseDelta` from the column on its left; the selected cycle is highlighted,
  a running cycle's expense bar is hatched and shows no change value, and the
  selected column scrolls into view;
- a dashed median tick in a **closed** selected column, from the served
  `comparison.expense.median3`; an active cycle shows none (partial against full);
- with fewer than four closed cycles the figure is a list of per-cycle cards
  instead of bars;
- a verdict of label : value pairs from served comparison fields: closed cycles
  compare with the previous full cycle and the 3-cycle median; active cycles
  compare with the same elapsed days. Missing baselines read "Belum ada pembanding".
  Direction is a signed value with a neutral ▲/▼ marker, never colour alone;
- selecting a column, or the Previous/Next buttons, changes `cycle=` in the URL
  and loads that cycle. The reviewed cycle also survives a Calendar visit, and the
  page title follows the view.

Below the bars, the **category x cycle matrix** uses the API's `categoryHistory`
in the same table, so its columns line up with the ribbon: the top categories plus
**Lainnya**, in Rp juta. Every cell is text. The tint sizes a value against the
largest positive value in its own row, so it is a visual length, never a good/bad
signal; zero and negative values carry no tint. A category row opens that
category's evidence (**Bukti kategori**) only when the served category facts
include it, so no click is a dead end; its button is named "Buka bukti …", apart
from the category buttons in the detail list. The matrix is not drawn when its
columns do not match `history`. With fewer than four closed cycles the same rows
appear in a standalone table under the cards. The ledger shows in the "Posisi
siklus" and "Perubahan" meeting steps.

The former "Apa yang berubah?" section is now the closed disclosure **Detail
perubahan** (open in the "Perubahan" meeting step). It still holds the baseline
bars, the ranked category deltas and the complete comparison table; nothing was
removed. Comparison labels now read "Selisih vs …" and "hari yang sama" instead of
"Δ" and "hari setara".

On phones the ribbon shows a whole number of cycle columns beside the sticky label
(four; three on the narrowest screens), sized from the figure's own width, so no
column is cut at the label edge. Columns snap to the label, the selected cycle starts
right after it on load (the end of the scroll lines up with a column), row labels
fit their column, and an edge shadow appears only while there is more to scroll to.

Colour follows the [brand guide](brand-guidelines.md): baseline and category-change
bars are neutral ink, a decrease is hatched rather than a different hue, selection
is butter, and the category distribution chart uses the chart ramp. Collapsed
sections say what is inside ("3 kategori · 8 merchant", "2 pencatat"), the daily
values table gains **Total sampai hari ini** (the pace chart's table equivalent),
and tables show edge shadows only while they overflow instead of a permanent
"geser" caption.

The browser formats numbers and maps amounts to bar lengths only. An absent
`history` renders nothing, so the page still works against an older API.

While another cycle loads, the previous report stays visible, dimmed, with a
"Memuat siklus…" status; a failed load clears it and shows the retry state, never
another cycle's numbers. The skeleton is only for the first load.

Under the daily bars, `#spending-shape` adds **Total pengeluaran sampai hari ini**:
a separate, single-series line of the served running total with its own axis. This
does not reverse the earlier chart refinement, which removed the cumulative line
from the *daily bar chart* because it dominated the scale; the daily bars are
unchanged. The previous cycle (dashed) and the 3-cycle median (dotted) are drawn as
curves from the API's `pace` series, so you can see whether this cycle is ahead of or
behind them on any day, not only at the end. Equal-day totals are markers on those
curves, and every value is listed as text below the chart. When the API serves no
curve (an older API, or no history) the chart falls back to the served comparison
totals as dashed levels and markers. If a daily row lacks the served running total,
the chart is replaced by a short "belum tersedia" line, never silently dropped.

**Posisi siklus** leads differently while a cycle is running: the headline is
**Pengeluaran bersih sejauh ini**, because salary lands on day 1 and a net cashflow
headline looks large until spending accumulates. Net cashflow stays visible as
**Arus kas bersih sejauh ini**, and a closed cycle still leads with **Arus kas
bersih**. On wide screens the daily-spending section now uses the full width
instead of sharing a row with the collapsed "Detail perubahan", which left the
right half empty.

The cycle header now says "hari ke-N, hari ini belum penuh" for an active cycle,
and the daily average is shown in whole rupiah like every other amount.

## Calendar view hygiene — October 2, 2026

The calendar view (`view=calendar`) is secondary to the cycle review; these fixes
keep it honest without redesigning it.

- A custom range is validated beside its fields with the API's own rules (both
  months, start not after end, at most 24 months, end at most next month); the
  message appears in an alert and an invalid range never reaches the URL. Inputs
  are controlled, so they follow the URL after Back or a preset, are bounded by
  `max`, and **Kustom** shows its selected state.
- When the API rejects a range, its reason is read and shown as guidance
  (for example "Rentang kustom harus 1 sampai 24 bulan…") instead of one fixed
  sentence.
- Months read "Sep 26", not `2026-09`. The running month is marked "· berjalan" in
  the table and chart tooltip, with a note under the chart. A month with no
  confirmed transactions says "belum ada transaksi" instead of Rp0.
- The monthly table is Bulan, Pemasukan, **Pengeluaran bersih** (already net of
  refunds), **Refund**, Arus kas bersih. The separate "Pengeluaran setelah refund"
  list and its `/analytics/spending` request are gone: they repeated the cashflow
  expense column.
- Each section (cashflow chart and table, categories, merchants, household notes)
  loads, fails and retries on its own. A failing request leaves the other sections
  intact and shows its error with a **Coba lagi** button; only a range that every
  section rejects shows a single notice. A new range keeps each section's previous
  data, dimmed ("Memuat rentang…"), until it arrives; a failed section clears its
  own numbers.

## Page order — October 2, 2026

The cycle view reads top to bottom as: **Siklus ke siklus** (ledger, verdict, category
matrix) → **Posisi siklus** → **Pola pengeluaran** (daily bars and the pace curves)
→ closed disclosures **Detail perubahan**, **Bukti kategori**, **Distribusi &
merchant**, **Catatan rumah tangga**, Savings and Wealth, quality, **Bahan
pembahasan** and the decisions. The numbered list under "Scan-first report" is the
inventory of facts, not the on-screen order.

Choosing a category (matrix row, change list or selector) opens **Bukti kategori**
without moving the reader: focus stays on the control that was used, and a status line
under the ledger names the category with **Lihat bukti** (moves focus to the evidence
on request) and **Hapus pilihan**. Earlier text below that says selection "opens and
focuses" the evidence is superseded by this. In a meeting step, choosing a category
still advances to its evidence step.

On wide screens (the section at least 820px wide) the daily-bars and pace charts sit
side by side; they stack below that. The selected ledger column eases in when motion
is allowed. While commentary is being prepared the card says the page checks every 5
seconds for up to about 7 minutes (90 polls, from `pollInsight`), which is the client's
polling budget, not a promise about generation time. A sliding column highlight from
the mock was not built: it adds nothing the highlighted column does not already show.

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
compact metric strip; daily spending and category changes once sat side-by-side on
wide screens (superseded: see "Cycle ledger"). Previous/current/median expense amounts
use directly labelled bars on one zero-based magnitude scale. Signed values stay
explicit; null history stays missing. Browser arithmetic only sizes visual bars.

Every category remains in the compact current/delta list in server order.
The full comparison table retains previous/median values, both absolute/relative
deltas and signed contribution under **Perbandingan lengkap**. Selecting a category opens
its evidence without a second API call (focus behaviour: see "Page order"). Merchant/transaction
tables remain household/cycle-bound and keyboard-scrollable.

Comparison context shows actual inclusive dates for the current measured range,
the full previous closed cycle, and the supplemental equal-day prefix. Active
amounts are labelled running, not complete. Full-cycle deltas lead category
rankings and cashflow context; equal-day deltas, median and contribution remain
explicitly labelled in the expanded table. Merchant details show both contexts.
Visual bars indicate relative magnitude, not percentage growth. Equal full-cycle
rent amounts show Rp0 / 0% even when the prior prefix has zero rent. Zero-baseline
ratios remain unavailable, even when a visual bar spans 100% width.

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
Closed cycles require exact `metrics.period_end`, including polling. Active cycles
may retain an earlier valid cutoff as `Snapshot sebelumnya` with its own inclusive
date range and `bukan data terkini` label, rather than disappearing at midnight.
Future/malformed cutoffs are excluded. Fresh commentary also shows its measured
date range. Generation errors retain the readable previous snapshot.
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
Worker READ sessions retain `facts_snapshot.generatedAt` across retries/midnight;
PostgreSQL retains the independent `created_at` clock for rate limiting.
New commentary uses `cycle-analyst-v5` and distinguishes full-cycle context from
supplemental elapsed comparisons. Older commentary stays preserved as historical;
no automatic regeneration is added.
Server reuse also checks the cutoff. HTTP 429 explains the preserved hourly cap;
HTTP 409 explains pending earlier work, rather than returning a mismatched ID.

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
