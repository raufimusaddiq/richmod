# Richmod — Restore LLM-Powered Analytics Insight in Web

**Document type:** Codex implementation plan + UX specification + reference snippets  
**Repository:** `raufimusaddiq/richmod`  
**Target:** latest `main` at implementation time  
**Scope:** expose the existing aggregate-only LLM insight capability in `/analytics` without changing deterministic chart ownership  
**Stack:** Go + Next.js + React + JavaScript + Recharts  
**Status:** implementation requested

> This is **not** a new LLM architecture.
>
> Richmod already has the insight backend, API, worker job, Cloud LLM Gateway flow,
> aggregate-only safety boundary, and persisted insight records.
>
> The task is to restore/expose that capability properly in the web analytics UI.

---

# 0. Codex startup instructions

Before editing:

1. pull latest `main`;
2. read root `AGENTS.md`;
3. inspect current web analytics implementation;
4. inspect the current insight API + worker implementation;
5. inspect current V2/V3/V4 docs and ADR-015;
6. preserve the latest chart refinement already present on `main`;
7. do not recreate an older chart implementation.

Read at minimum:

```text
AGENTS.md

docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md
docs/RICHMOD_PRODUCT_ITERATION_V3.md
docs/V3_IMPLEMENTATION_CHECKLIST.md
docs/adr/ADR-015-aggregate-only-llm-insights.md

apps/api/internal/insight/handler.go
apps/worker/internal/insight/processor.go

apps/web/app/analytics/page.js
apps/web/app/components/Charts.js
apps/web/app/lib/chartData.js
apps/web/app/globals.css
```

The latest frontend already includes chart concepts such as:

```text
CycleSpendingPatternChart
MonthlyCashflowChart
CategoryRankingChart
chartData.js helpers
```

Preserve them.

Use the repository branch + linked worktree workflow required by `AGENTS.md`.

---

# 1. Existing Richmod insight architecture

The current architecture must remain:

```text
PostgreSQL / Go
        ↓
deterministic aggregate financial facts
        ↓
persisted insight snapshot
        ↓
BACKGROUND job: GENERATE_INSIGHT
        ↓
Cloud LLM Gateway
        ↓
strict Indonesian narrative result
        ↓
Go validation
        ↓
persisted non-authoritative insight text
        ↓
Web / Telegram presentation
```

The LLM is **not** the source of truth for numbers.

The model must not:

```text
calculate canonical transaction amounts
query raw transaction rows directly
receive raw evidence
receive account identifiers
receive raw user messages
mutate financial state
decide transaction status
replace SQL/Go analytics
```

ADR-015 remains the governing insight architecture:

```text
aggregate deterministic facts only
→ LLM narrative
→ non-authoritative observations
```

---

# 2. Existing APIs to reuse

Do not create a duplicate insight API.

Reuse:

```http
GET /api/v1/insights
POST /api/v1/insights/generate
```

The backend already supports salary-cycle generation via:

```http
POST /api/v1/insights/generate?period=cycle
```

The generation API may return:

```text
202 PENDING
```

or:

```text
200 EXISTING
```

Both are valid successful states.

The worker already owns:

```text
GENERATE_INSIGHT
```

Do not introduce a direct browser → LLM request.

---

# 3. Product goal

Current `/analytics` should become:

```text
deterministic facts
+
deterministic visualization
+
LLM interpretation of those facts
```

The page must visually communicate this hierarchy:

```text
KPI
↓
Deterministic chart
↓
✦ Analisis Richmod
↓
Deterministic category / merchant / member breakdown
```

The LLM section is supplemental.

If the LLM is pending or failed, all charts and deterministic analytics must remain available.

---

# P0.1 — Add `Analisis Richmod` card to `/analytics`

**Priority:** P0 UX

Recommended placement:

```text
Mode controls

KPI cards

Main deterministic chart

Analisis Richmod      ← NEW

Category ranking
Merchant ranking
Member contribution
```

Do not place the card above the main chart.

The intended information order is:

```text
show facts first
→ explain the facts second
```

---

# 4. Final visual specification

Desktop concept:

```text
┌────────────────────────────────────────────────────────────┐
│ ANALISIS                                                   │
│ Pola Keuangan Rumah Tangga                                 │
│                                                            │
│ [ Siklus Gaji ] [ Kalender ]                               │
│                                                            │
│ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌────────┐ │
│ │ Rata-rata   │ │ Hari        │ │ Hari tanpa  │ │ Siklus │ │
│ │ / hari      │ │ tertinggi   │ │ pengeluaran │ │ jalan  │ │
│ │ Rp63.748    │ │ 27 Agu      │ │ 1 hari      │ │ 6/31   │ │
│ └─────────────┘ └─────────────┘ └─────────────┘ └────────┘ │
│                                                            │
│ ┌────────────────────────────────────────────────────────┐ │
│ │ SIKLUS GAJI · HARIAN                                  │ │
│ │ Pola pengeluaran siklus ini                            │ │
│ │                                                        │ │
│ │       █                                                │ │
│ │ █     █          █                                     │ │
│ │ █ ─ ─ ─ ─ ─ ─ ─ ─ ─   Rata-rata Rp63.748            │ │
│ │ █     █   █      █   █                                 │ │
│ │ ─────────────────────────────────────────              │ │
│ │ 24   25   26   27   28   29                           │ │
│ └────────────────────────────────────────────────────────┘ │
│                                                            │
│ ┌────────────────────────────────────────────────────────┐ │
│ │ ✦ ANALISIS RICHMOD                       [ Perbarui ] │ │
│ │ Ringkasan pola keuangan                                │ │
│ │                                                        │ │
│ │ Pengeluaran pada siklus ini cukup terkonsentrasi       │ │
│ │ pada kebutuhan makanan dan rumah tangga.               │ │
│ │                                                        │ │
│ │ • Pengeluaran tertinggi terjadi pada 27 Agustus.       │ │
│ │ • Belanja Bahan Makanan menjadi kategori terbesar.     │ │
│ │ • Pengeluaran meningkat dibanding siklus sebelumnya.   │ │
│ │                                                        │ │
│ │ Kualitas data · Tinggi · 96%                           │ │
│ │ Diperbarui 29 Agu 2026 · 10:42                         │ │
│ └────────────────────────────────────────────────────────┘ │
│                                                            │
│ [ Peringkat kategori ] [ Merchant ] [ Kontribusi anggota ]│
└────────────────────────────────────────────────────────────┘
```

This should feel like an **analyst note**, not a chatbot message.

---

# 5. Visual language

Use the same Richmod surface-card language already present:

```text
white surface
existing border/radius
existing typography
existing spacing rhythm
restrained neutral/green palette
```

Do not create:

```text
chat bubbles
large AI badge
neon gradient
glowing border
assistant avatar
typewriter animation
```

Preferred header:

```text
✦ ANALISIS RICHMOD
Ringkasan pola keuangan
```

The `✦` should be subtle.

Avoid product labels such as:

```text
AI Advice
Financial Advice
Rekomendasi Finansial
```

because this output is descriptive/non-authoritative.

---

# P0.2 — Load persisted insight before generating anything

Do not automatically generate an insight on page load.

On `/analytics` load:

```http
GET /api/v1/insights
```

Select the most relevant persisted insight for the active mode.

For:

```js
mode === "cycle"
```

prefer the current salary-cycle insight.

Do not merely display the newest arbitrary insight if it belongs to a different period.

If no suitable insight exists:

```text
✦ ANALISIS RICHMOD
Ringkasan pola keuangan

Belum ada analisis untuk siklus ini.
Richmod dapat membaca pola dari data keuangan
yang sudah terkonfirmasi.

[Buat analisis]
```

---

# P0.3 — Explicit generation / refresh

Generation must be user-triggered.

Buttons:

```text
[Buat analisis]
```

or:

```text
[Perbarui]
```

For cycle mode:

```js
await fetch(
  "/api/v1/insights/generate?period=cycle",
  { method: "POST" },
);
```

For supported current-calendar-month mode:

```js
await fetch(
  "/api/v1/insights/generate",
  { method: "POST" },
);
```

Do not generate on:

```text
React render
browser refresh
chart hover
tab mount
range button hover
every analytics fetch
```

Reuse persisted insight whenever possible.

---

# P0.4 — Bounded polling

Generation is asynchronous.

Expected flow:

```text
POST generate
    ↓
PENDING / EXISTING
    ↓
GET /api/v1/insights
    ↓
PENDING
    ↓
poll
    ↓
SUCCEEDED / FAILED
```

Polling must be bounded.

Suggested behavior:

```text
interval: 1.5–2 seconds
max duration: ~20–30 seconds
stop immediately on final status
stop on component unmount
stop if active mode/period changes
```

Do not create an endless interval.

---

# P0.5 — UI states

The card must explicitly support all states.

## No existing insight

```text
✦ ANALISIS RICHMOD
Ringkasan pola keuangan

Belum ada analisis untuk siklus ini.
Richmod dapat membaca pola dari data keuangan
yang sudah terkonfirmasi.

[Buat analisis]
```

## Pending

```text
✦ ANALISIS RICHMOD
Ringkasan pola keuangan

Sedang membaca pola keuangan siklus ini…

[Menganalisis…]
```

Button disabled.

Do not block deterministic charts.

## Succeeded

```text
✦ ANALISIS RICHMOD
Ringkasan pola keuangan                         [Perbarui]

Pengeluaran pada siklus ini cukup terkonsentrasi
pada kebutuhan makanan dan rumah tangga.

• Pengeluaran tertinggi terjadi pada 27 Agustus.
• Belanja Bahan Makanan menjadi kategori terbesar.
• Pengeluaran meningkat dibanding siklus sebelumnya.

Kualitas data · Tinggi · 96%
Diperbarui 29 Agu 2026 · 10:42
```

## Failed

```text
✦ ANALISIS RICHMOD
Ringkasan pola keuangan

Analisis belum berhasil dibuat.
Grafik dan data keuangan tetap tersedia.

[Coba lagi]
```

Do not show raw gateway/provider errors to household users.

---

# P0.6 — Data completeness UX

Preserve backend behavior.

Current architecture intentionally uses deterministic data quality checks.

If completeness is below the backend LLM threshold:

```text
do not force an LLM call
```

Render the deterministic warning as a valid insight result.

Suggested quality labels:

```js
function completenessLabel(value) {
  const ratio = Number(value || 0);

  if (ratio >= 0.90) return "Tinggi";
  if (ratio >= 0.70) return "Cukup";
  return "Perlu dilengkapi";
}
```

Display:

```text
Kualitas data · Tinggi · 96%
```

or:

```text
Kualitas data · Cukup · 82%
```

or:

```text
Kualitas data · Perlu dilengkapi · 64%
```

For incomplete data:

```text
Data belum cukup lengkap untuk membuat analisis
yang andal.

Selesaikan transaksi yang masih perlu ditinjau
agar pola pengeluaran lebih representatif.
```

Do not repeatedly retry the model when the backend intentionally bypassed it because data was incomplete.

---

# P0.7 — Safe insight text rendering

Treat persisted LLM text as plain text.

Do not use:

```js
dangerouslySetInnerHTML
```

Reference:

```jsx
function InsightText({ text }) {
  return (
    <div className="insight-text">
      {String(text || "")
        .split(/
{2,}/)
        .filter(Boolean)
        .map((paragraph, index) => (
          <p key={index}>{paragraph}</p>
        ))}
    </div>
  );
}
```

Do not build a new HTML/Markdown execution surface solely for this feature.

---

# 6. Suggested component split

Preferred new component:

```text
apps/web/app/components/InsightCard.js
```

Possible props:

```jsx
<InsightCard
  insight={insight}
  loading={insightLoading}
  error={insightError}
  canGenerate={canGenerate}
  onGenerate={generateInsight}
/>
```

The component must not decide financial period semantics.

Keep period selection/matching in the page or a deterministic helper.

---

# 7. Suggested frontend state

Reference only:

```js
const [insight, setInsight] = useState(null);
const [insightLoading, setInsightLoading] = useState(false);
const [insightError, setInsightError] = useState("");
```

Loader:

```js
async function loadInsights(signal) {
  const response = await fetch(
    "/api/v1/insights",
    { signal },
  );

  if (!response.ok) {
    throw new Error("unable to load insights");
  }

  return response.json();
}
```

Generation:

```js
async function requestInsight(mode) {
  const suffix =
    mode === "cycle"
      ? "?period=cycle"
      : "";

  const response = await fetch(
    `/api/v1/insights/generate${suffix}`,
    { method: "POST" },
  );

  if (!response.ok) {
    throw new Error("unable to generate insight");
  }

  return response.json();
}
```

Handle both `PENDING` and `EXISTING` as success.

---

# 8. Period matching

Do not select an insight only by array position.

Use deterministic period matching.

For cycle mode, compare against the active cycle metadata where available:

```text
period_kind
period_start
period_end
```

The persisted `metrics` snapshot should be used when it contains those facts.

Do not show an insight from a previous salary cycle as if it belongs to the current one.

---

# 9. Calendar-mode limitation

Current analytics UI supports:

```text
3 Bulan
6 Bulan
12 Bulan
Kustom
```

Do not misrepresent a current-month LLM insight as a multi-month LLM analysis.

For this iteration:

```text
Siklus Gaji
→ full Analisis Richmod support

Kalender multi-month / custom range
→ hide the card or clearly say range insight is not yet available
```

Preferred temporary UX:

```text
Analisis Richmod untuk rentang beberapa bulan
belum tersedia.
```

Do not expand the backend into arbitrary range-aware LLM analysis in this UI restoration task unless the latest `main` already supports it cleanly.

---

# 10. Deterministic chart vs LLM responsibility

These remain deterministic:

```text
Rata-rata / hari
Hari tertinggi
Hari tanpa pengeluaran
Siklus berjalan
Income
Expense
Net cashflow
Category ranking
Merchant ranking
Member contribution
```

They come from:

```text
SQL / Go / deterministic JavaScript derivation
```

The model may interpret patterns, but must not become the source for chart numbers.

---

# 11. Preserve current chart refinement

Do not restore old chart behavior.

Preserve:

```text
Dashboard:
daily spending bars

Analytics / Siklus:
CycleSpendingPatternChart
daily bars + average reference

Analytics / Kalender:
MonthlyCashflowChart
grouped income vs expense

Analytics category:
CategoryRankingChart
```

Do not restore:

```text
daily cumulative expense line
3-series income/expense/net plot on one scale
crowded analytics donut
```

The LLM card is additive.

---

# 12. Metadata shown to users

Show:

```text
data quality/completeness
completed time
```

Do not prominently show:

```text
model
gatewayRoute
promptVersion
internal UUID
raw input metrics
```

Those are ops/admin metadata.

---

# 13. CSS direction

Use existing tokens first.

Illustrative only:

```css
.insight-card {
  display: grid;
  gap: 18px;
}

.insight-text {
  display: grid;
  gap: 10px;
  max-width: 900px;
}

.insight-text p {
  margin: 0;
  line-height: 1.65;
}

.insight-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
  color: var(--muted);
  font-size: 13px;
}
```

No visual redesign beyond this feature.

---

# 14. Mobile behavior

On narrow screens:

```text
header/title
↓
Perbarui button
↓
summary
↓
observations
↓
quality
↓
updated time
```

Do not create horizontal overflow.

---

# 15. Accessibility

Requirements:

```text
generation button has visible text
disabled loading state is clear
loading status is readable
quality is not represented only by color
model narrative remains selectable text
```

Optional:

```jsx
<div aria-live="polite">
  {loading &&
    "Sedang membaca pola keuangan siklus ini…"}
</div>
```

Avoid announcements on every polling tick.

---

# 16. Error isolation

Insight API failure must not fail the whole analytics page.

Bad:

```text
GET /insights fails
→ entire analytics page error
```

Good:

```text
charts render normally
+
Analisis Richmod card shows local error
```

---

# 17. Cost and performance

Do not generate automatically.

Reuse persisted insight.

Polling must be bounded and cancelable.

`GENERATE_INSIGHT` remains background work.

Do not move it to the INTERACTIVE lane.

Do not add another LLM request for chart explanation on every page view.

---

# 18. Security invariants

Must remain:

```text
browser never calls provider directly

LLM receives aggregate deterministic facts only

raw bank email does not enter insight generation

raw evidence does not enter insight generation

raw transaction rows are not sent to the model

account identifiers are not sent

LLM cannot mutate ledger state

insight text is non-authoritative
```

---

# 19. Tests

Add/update frontend tests for:

```text
no insight → CTA

PENDING → loading state

EXISTING response accepted

SUCCEEDED → narrative visible

FAILED → retry state

completeness labels

cycle generation uses ?period=cycle

bounded polling stops on success/failure/timeout

polling cleanup on unmount/mode change

no dangerouslySetInnerHTML

calendar multi-month mode does not claim
a single-month insight covers selected range
```

Run:

```bash
cd apps/web
npm test
npm run build
```

If backend Go code changes, run relevant API/worker Go tests too.

---

# 20. Definition of Done

```text
[ ] Existing insight backend/API/worker architecture is reused.

[ ] No direct browser → LLM request exists.

[ ] `/analytics` shows Analisis Richmod after the main deterministic chart.

[ ] The card looks like an analyst note, not a chatbot.

[ ] Relevant persisted cycle insight is loaded.

[ ] No-insight state shows "Buat analisis".

[ ] Cycle generation calls:
    POST /api/v1/insights/generate?period=cycle

[ ] PENDING is handled.

[ ] EXISTING is treated as success.

[ ] Polling is bounded and canceled correctly.

[ ] SUCCEEDED text is rendered safely.

[ ] FAILED does not break charts.

[ ] Data quality is shown in Indonesian.

[ ] Backend completeness gate remains intact.

[ ] Charts and KPI values remain deterministic.

[ ] Current chart refinement remains intact.

[ ] Multi-month calendar mode does not falsely present a one-month LLM insight.

[ ] Model/gateway/internal metadata is not primary household UX.

[ ] JavaScript only; no TypeScript migration.

[ ] npm test passes.

[ ] npm run build passes.

[ ] No unrelated financial-state changes.

[ ] Branch/worktree workflow follows AGENTS.md.
```

---

# 21. Final target page

```text
ANALISIS
Pola Keuangan Rumah Tangga

[Siklus Gaji] [Kalender]

[KPI] [KPI] [KPI] [KPI]

┌──────────────────────────────────────────┐
│ SIKLUS GAJI · HARIAN                    │
│ Pola pengeluaran siklus ini              │
│                                          │
│ deterministic Recharts chart             │
└──────────────────────────────────────────┘

┌──────────────────────────────────────────┐
│ ✦ ANALISIS RICHMOD           [Perbarui] │
│ Ringkasan pola keuangan                  │
│                                          │
│ generated narrative based only on        │
│ deterministic aggregate facts            │
│                                          │
│ Kualitas data · Tinggi · 96%             │
│ Diperbarui 29 Agu 2026 · 10:42           │
└──────────────────────────────────────────┘

[Peringkat kategori]
[Merchant]
[Kontribusi anggota]
```

---

# 22. Codex completion report

When finished, report:

```text
baseline main SHA
branch
worktree path
files changed
whether backend changed
InsightCard state handling
cycle period matching
polling bounds
tests + results
web build result
manual desktop/mobile verification
commit SHA
merge SHA
pushed main SHA
remaining limitation for calendar multi-month insight
```

Do not claim completion if the UI can display an insight belonging to the wrong financial period.
