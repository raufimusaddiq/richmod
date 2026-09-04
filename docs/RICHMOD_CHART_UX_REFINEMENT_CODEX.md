# Richmod Chart UX Refinement — Codex Implementation Plan

**Repository:** `raufimusaddiq/richmod`  
**Target:** current `main` at implementation time  
**Baseline reviewed:** `main` @ `e799f1713a09d6469a74621289e55f3ae2ddfc77`  
**Baseline commit:** `merge: fix canonical review ignore resolution`  
**Review date:** 2026-08-29  
**Scope:** Ringkasan + Analisis chart semantics, chart components, derived frontend analytics, responsive behavior, and chart tests  
**Stack:** Next.js + React + JavaScript + Recharts  
**Status:** implementation requested

> This is one cohesive chart-refinement task.
>
> Do not split C1–C5 into independent feature branches unless there is a concrete
> reason. They touch the same chart components and page composition and are easier
> to keep visually consistent in one branch/worktree.
>
> This work is not V5 and does not change the Richmod ledger architecture.

---

# 0. Current `main` review

The repository was re-checked before writing this plan.

Current `main` head reviewed:

```text
e799f1713a09d6469a74621289e55f3ae2ddfc77
merge: fix canonical review ignore resolution
```

Recent commits after the previous product review mainly concern:

```text
canonical review ignore resolution
job retry logging
Telegram message polish
```

The chart implementation itself is still materially the same as the version
reviewed earlier.

Relevant current files:

```text
AGENTS.md
apps/web/app/page.js
apps/web/app/analytics/page.js
apps/web/app/components/Charts.js
apps/web/app/globals.css
apps/web/app/lib/format.js
apps/web/tests/product-alignment.test.mjs
```

Current chart components:

```text
CashflowChart
CategoryChart
DailyCashflowChart
```

Current frontend dependencies already include:

```text
recharts 3.10.1
```

Do not add another chart library.

---

# 1. Repository rules Codex must follow

Read root `AGENTS.md` before editing.

Current project rules include:

```text
Backend: Go
Frontend: Next.js + React + JavaScript
Database: PostgreSQL
No TypeScript app migration
No new infrastructure for this task
Run relevant tests before marking complete
```

Every repository-changing request must use its own branch and linked worktree.

Suggested workflow:

```bash
git checkout main
git pull --ff-only

git worktree add   -b feat/chart-ux-refinement   ../family-finance-worktrees/chart-ux-refinement   main
```

Perform all edits, tests, diff review, commits, and build verification in that
worktree.

After successful verification:

```bash
git push -u origin feat/chart-ux-refinement

git checkout main
git pull --ff-only
git merge --no-ff feat/chart-ux-refinement
git push origin main
```

Deploy only if explicitly requested.

---

# 2. Product distinction that must drive the charts

The key product problem is not simply "the charts look bad."

The current charts mix metrics that answer different financial questions.

Richmod should keep a clear distinction:

```text
RINGKASAN
"What is our financial position right now?"

ANALISIS · SIKLUS GAJI
"What spending pattern has happened since payday?"

ANALISIS · KALENDER
"How is household cash flow changing across months?"
```

Do not repeat the same hero cash-flow visualization on both Ringkasan and
Analisis.

---

# 3. Current UX problems

## 3.1 Ringkasan cash-flow scale distortion

Current home page loads:

```text
/api/v1/analytics/overview
/api/v1/analytics/cycle/daily
/api/v1/analytics/categories?range=3
/api/v1/transactions?limit=8
/api/v1/analytics/cycle
```

and passes:

```jsx
<CashflowChart items={cashflow}/>
```

into the Ringkasan hero chart.

`CashflowChart` plots:

```text
incomeValue
expenseValue
netValue
```

on one Y scale.

In a normal salary cycle:

```text
salary/income event = many millions
daily expense       = tens/hundreds of thousands
```

so expenses become visually almost flat.

This is mathematically valid but analytically weak.

## 3.2 Analytics cycle mixes daily and cumulative measures

Current `DailyCashflowChart` plots:

```text
bar  = expenseValue
line = cumulativeValue
```

on the same visual scale.

`cumulativeValue` almost always rises and eventually becomes much larger than one
day's expense.

It therefore dominates the chart and makes the daily pattern difficult to read.

## 3.3 Analytics repeats Ringkasan instead of adding analysis

Current analytics cycle KPIs are generic:

```text
Total Pengeluaran
Pengembalian Dana
Merchant Terbesar
Kategori Terbesar
```

while the main cycle chart is still called:

```text
Arus kas siklus berjalan
```

This does not provide a sufficiently distinct analytical view from Ringkasan.

## 3.4 Category donut and legend can disagree

Current `CategoryChart`:

```text
Pie data      = all categories
Legend rows   = data.slice(0, 6)
```

If there are more than six categories, the donut can contain slices with no
visible legend entry.

The six-color palette is also reused for additional slices.

## 3.5 Generic component names hide financial meaning

Current names:

```text
CashflowChart
DailyCashflowChart
```

do not make it clear which product question the chart answers.

Prefer explicit components aligned to page semantics.

---

# 4. Final chart architecture

Target chart components:

```text
DashboardDailySpendingChart
CycleSpendingPatternChart
MonthlyCashflowChart
CategoryDonutChart
CategoryRankingChart
```

Recommended ownership:

```text
Charts.js
    visual components

analytics/chartData.js or equivalent helper module
    pure frontend data derivation helpers

page.js / analytics/page.js
    page composition + fetching
```

If the repository prefers keeping pure helpers in `Charts.js`, that is acceptable
for a small implementation, but exported pure helpers are easier to test.

Do not introduce a state-management library.

---

# C1 — Ringkasan: make the hero chart a compact spending preview

**Priority:** P0 UX  
**Files likely affected:**

```text
apps/web/app/page.js
apps/web/app/components/Charts.js
apps/web/app/globals.css
apps/web/tests/product-alignment.test.mjs
```

## Current behavior

Home currently shows:

```text
Pemasukan siklus ini
Pengeluaran siklus ini
Arus kas bersih
Perlu Ditinjau
```

and then a chart with:

```text
Pemasukan
Pengeluaran
Arus bersih
```

again.

The KPI cards already answer the high-level cash-position question.

The chart does not need to repeat all three.

## Final question answered

> "Kapan uang keluar selama periode aktif?"

## Final visual

Use a compact bar chart:

```text
bar = daily expense
x   = day/date
```

No:

```text
income line
net line
cumulative line
dual axis
log scale
```

### Copy

When the active period is a salary cycle:

```text
Eyebrow: SIKLUS GAJI · HARIAN
Title:   Pengeluaran harian
```

If the active period falls back to calendar month:

```text
Eyebrow: BULAN INI · HARIAN
Title:   Pengeluaran harian
```

Do not hardcode `SIKLUS GAJI` when the API says the current period is calendar.

Keep:

```text
Lihat analisis →
```

## Tooltip

Example:

```text
29 Agu 2026
Pengeluaran · Rp 85.000
```

Use a full localized date when possible.

The X axis may remain compact:

```text
24
25
26
...
```

## Modification plan

1. Create `DashboardDailySpendingChart`.
2. Map existing `/analytics/cycle/daily` rows to:
   ```text
   date
   label
   expenseValue
   ```
3. Update `page.js` import.
4. Replace `CashflowChart` in the dashboard hero.
5. Make eyebrow period-aware using the already-fetched `cycle.kind`.
6. Preserve empty/loading behavior.
7. Keep current Ringkasan KPI cards.
8. Do not add a backend API.

## Reference code sketch

```jsx
export function DashboardDailySpendingChart({
  items,
  height = 280,
}) {
  const data = items.map(item => ({
    ...item,
    label: item.period?.slice(8) || "",
    expenseValue: Number(item.expense || 0),
  }));

  if (!data.length) {
    return (
      <p className="empty compact">
        Belum ada pengeluaran pada periode ini.
      </p>
    );
  }

  return (
    <div
      className="chart-wrap"
      role="img"
      aria-label="Grafik pengeluaran harian"
      style={{ height }}
    >
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={data}
          margin={{ top: 10, right: 12, left: 0, bottom: 0 }}
          barCategoryGap="28%"
        >
          <CartesianGrid stroke="#e8ece7" vertical={false} />
          <XAxis
            dataKey="label"
            axisLine={false}
            tickLine={false}
            tick={{ fill: "#6f786f", fontSize: 11 }}
            interval={data.length > 14 ? 2 : 0}
          />
          <YAxis hide />
          <Tooltip
            formatter={value => [
              money(String(Math.round(value))),
              "Pengeluaran",
            ]}
            labelFormatter={label => `Tanggal ${label}`}
          />
          <Bar
            dataKey="expenseValue"
            fill="#d47a66"
            radius={[4, 4, 0, 0]}
            maxBarSize={24}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
```

Page composition sketch:

```jsx
const activePeriodEyebrow =
  cycle?.kind === "CURRENT_CYCLE"
    ? "SIKLUS GAJI · HARIAN"
    : "BULAN INI · HARIAN";

...

<article className="surface chart-panel">
  <div className="section-title">
    <div>
      <span className="eyebrow">{activePeriodEyebrow}</span>
      <h2>Pengeluaran harian</h2>
    </div>

    <Link href="/analytics">
      Lihat analisis →
    </Link>
  </div>

  <DashboardDailySpendingChart items={cashflow}/>
</article>
```

## Acceptance tests

```text
income magnitude never affects dashboard chart scale
net cashflow is not rendered in the hero chart
zero-expense day remains represented
empty daily data shows useful empty state
calendar fallback does not say "Siklus Gaji"
tooltip formats money as IDR
desktop and mobile remain readable
```

---

# C2 — Analisis / Siklus Gaji: show spending pattern, not cumulative total

**Priority:** P0 UX  
**Files likely affected:**

```text
apps/web/app/analytics/page.js
apps/web/app/components/Charts.js
apps/web/app/globals.css
apps/web/tests/product-alignment.test.mjs
```

## Final question answered

> "Hari mana pengeluaran tinggi atau rendah sejak gajian?"

## Final hero chart

Create:

```text
CycleSpendingPatternChart
```

Visual:

```text
bars            = daily expense
reference line  = average spending per elapsed calendar day
```

Remove:

```text
cumulative expense line
legend for one bar series
income
net cashflow
```

## Average definition

Use:

```text
daily average = total spent / max(daysElapsed, 1)
```

Do **not** calculate average only across days that have transactions.

## Peak day

Derived from:

```text
max(daily.expense)
```

Do not require a backend endpoint.

## Zero-spend days

Count elapsed cycle days whose daily expense is zero.

Important:

If the daily API already returns every elapsed day including zero values, count
directly.

If it only returns transaction days, do not silently claim zero-spend day count
is correct. Inspect the API contract first.

If needed, derive missing elapsed dates from explicit cycle bounds only when the
current API contract supports it.

Prefer fixing the backend daily-series contract if it is supposed to be a dense
daily timeline but currently is sparse.

Do not guess silently.

## Final cycle KPI cards

When `mode === "cycle"`, replace the generic four cards with:

```text
Rata-rata / hari
Hari tertinggi
Hari tanpa pengeluaran
Siklus berjalan
```

Example:

```text
Rata-rata / hari
Rp 63.748

Hari tertinggi
27 Agu · Rp 142.000

Hari tanpa pengeluaran
1 hari

Siklus berjalan
6 / 31 hari
```

Merchant/category information remains lower in the detailed analysis area.

## Hero copy

```text
Eyebrow: SIKLUS GAJI · HARIAN
Title:   Pola pengeluaran siklus ini
```

## Modification plan

1. Import `ReferenceLine` from Recharts.
2. Create pure helper `deriveCycleSpendingMetrics(...)`.
3. Create `CycleSpendingPatternChart`.
4. Remove cumulative line from cycle hero.
5. Pass `items`, `spent`, and `daysElapsed`.
6. Change cycle-specific KPIs.
7. Keep categories/merchant/member sections below.
8. Ensure category and merchant lists are not duplicated as KPI cards in cycle mode.
9. Do not add a backend endpoint for metrics already derivable from `dailyCycle`.

## Reference helper

```js
export function deriveCycleSpendingMetrics({
  daily = [],
  spent = "0",
  daysElapsed = 0,
  daysTotal = 0,
}) {
  const elapsed = Math.max(Number(daysElapsed || 0), 0);

  const normalized = daily.map(item => ({
    ...item,
    expenseValue: Number(item.expense || 0),
  }));

  const average =
    Number(spent || 0) / Math.max(elapsed, 1);

  const peak = normalized.reduce(
    (best, item) =>
      item.expenseValue > best.expenseValue
        ? item
        : best,
    { period: null, expenseValue: 0 },
  );

  const zeroSpendDays = normalized
    .slice(0, elapsed || normalized.length)
    .filter(item => item.expenseValue === 0)
    .length;

  return {
    average,
    peak,
    zeroSpendDays,
    daysElapsed: elapsed,
    daysTotal: Number(daysTotal || 0),
  };
}
```

If API daily data is not dense, correct the zero-day logic based on the actual
contract instead of using the helper above blindly.

## Reference chart

```jsx
export function CycleSpendingPatternChart({
  items,
  spent,
  daysElapsed,
  height = 340,
}) {
  const data = items.map(item => ({
    ...item,
    label: item.period?.slice(8) || "",
    expenseValue: Number(item.expense || 0),
  }));

  if (!data.length) {
    return (
      <p className="empty compact">
        Belum ada pengeluaran pada siklus aktif.
      </p>
    );
  }

  const average =
    Number(spent || 0) /
    Math.max(Number(daysElapsed || 0), 1);

  return (
    <div
      className="chart-wrap"
      role="img"
      aria-label="Pola pengeluaran harian siklus gaji"
      style={{ height }}
    >
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={data}
          margin={{ top: 20, right: 12, left: 0, bottom: 0 }}
          barCategoryGap="24%"
        >
          <CartesianGrid stroke="#e8ece7" vertical={false} />
          <XAxis
            dataKey="label"
            axisLine={false}
            tickLine={false}
            tick={{ fill: "#6f786f", fontSize: 11 }}
            interval={data.length > 14 ? 2 : 0}
          />
          <YAxis hide />
          <Tooltip
            formatter={value => [
              money(String(Math.round(value))),
              "Pengeluaran",
            ]}
            labelFormatter={label => `Tanggal ${label}`}
          />
          <ReferenceLine
            y={average}
            stroke="#7188a8"
            strokeDasharray="5 5"
            ifOverflow="extendDomain"
            label={{
              value: `Rata-rata ${money(String(Math.round(average)))}`,
              position: "insideTopRight",
              fill: "#6f786f",
              fontSize: 11,
            }}
          />
          <Bar
            dataKey="expenseValue"
            fill="#d47a66"
            radius={[4, 4, 0, 0]}
            maxBarSize={24}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
```

## Acceptance tests

```text
no cumulative line in cycle hero
average uses elapsed calendar days
average handles 0 elapsed days safely
peak day is correct
zero-spend count follows actual daily-series contract
single-day cycle renders
all-zero data renders without NaN
tooltip uses IDR
no unnecessary legend for one bar series
```

---

# C3 — Analisis / Kalender: use monthly grouped comparison

**Priority:** P1 UX  
**Files likely affected:**

```text
apps/web/app/analytics/page.js
apps/web/app/components/Charts.js
apps/web/tests/product-alignment.test.mjs
```

## Final question answered

> "Bagaimana pemasukan dan pengeluaran berubah antar bulan?"

## Final chart

Create:

```text
MonthlyCashflowChart
```

Use grouped bars:

```text
income
expense
```

Do not use the current filled 3-series area chart as the primary monthly
comparison.

`netCashflow` is derived from them and does not need a third visual series.

## Net display

Keep net cashflow in:

```text
tooltip
or calendar KPI card
```

not as a third line in the primary chart.

## Hero copy

```text
Eyebrow: TREND BULANAN
Title:   Pemasukan vs pengeluaran
```

## Calendar KPI cards

Recommended:

```text
Total Pemasukan
Total Pengeluaran
Arus Kas Bersih
Pengembalian Dana
```

If current page state does not hold total income directly, derive it from
`data.cashflow`.

Do not use merchant/category as hero KPIs in calendar mode; those already have
their own ranked breakdown.

## Modification plan

1. Create `MonthlyCashflowChart`.
2. Map calendar `cashflow` rows to `incomeValue`, `expenseValue`, `netValue`, `label`.
3. Render only income + expense bars.
4. Show net in tooltip.
5. Replace calendar usage of `CashflowChart`.
6. Use calendar-specific KPI cards.
7. Preserve 3/6/12 month + custom controls.

## Reference chart

```jsx
export function MonthlyCashflowChart({
  items,
  height = 340,
}) {
  const data = items.map(item => ({
    ...item,
    label: monthLabel(item.period),
    incomeValue: Number(item.income || 0),
    expenseValue: Number(item.expense || 0),
    netValue: Number(item.netCashflow || 0),
  }));

  if (!data.length) {
    return (
      <p className="empty compact">
        Belum ada data bulanan pada periode ini.
      </p>
    );
  }

  return (
    <div
      className="chart-wrap"
      role="img"
      aria-label="Perbandingan pemasukan dan pengeluaran bulanan"
      style={{ height }}
    >
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={data}
          margin={{ top: 12, right: 12, left: 0, bottom: 0 }}
          barCategoryGap="24%"
        >
          <CartesianGrid stroke="#e8ece7" vertical={false} />
          <XAxis
            dataKey="label"
            axisLine={false}
            tickLine={false}
            tick={{ fill: "#6f786f", fontSize: 12 }}
          />
          <YAxis hide />
          <Tooltip content={<MonthlyCashflowTooltip/>} />
          <Legend
            formatter={value => ({
              incomeValue: "Pemasukan",
              expenseValue: "Pengeluaran",
            })[value]}
          />
          <Bar
            dataKey="incomeValue"
            fill="#2d6a4f"
            radius={[4, 4, 0, 0]}
            maxBarSize={34}
          />
          <Bar
            dataKey="expenseValue"
            fill="#c86b55"
            radius={[4, 4, 0, 0]}
            maxBarSize={34}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
```

## Acceptance tests

```text
calendar hero has two visual series only
income/expense are grouped, not stacked
net is available in tooltip/KPI
3/6/12 month controls still work
custom date range still works
empty calendar data renders correctly
```

---

# C4 — Category visualizations: compact donut on Ringkasan, ranking in Analisis

**Priority:** P1 UX

## C4.1 Ringkasan category donut

Keep a donut on Ringkasan because it is a compact overview.

Normalize data first:

```text
Top 5 categories
+
Lainnya
```

Maximum:

```text
6 slices
```

The legend must render the exact same compact dataset.

## Reference helper

```js
export function compactCategories(items, limit = 5) {
  const sorted = [...items]
    .filter(item => Number(item.amount || 0) > 0)
    .sort(
      (a, b) =>
        Number(b.amount || 0) -
        Number(a.amount || 0),
    );

  const total = sorted.reduce(
    (sum, item) => sum + Number(item.amount || 0),
    0,
  );

  const primary = sorted.slice(0, limit);
  const rest = sorted.slice(limit);
  const compact = [...primary];

  if (rest.length) {
    compact.push({
      id: "other",
      name: "Lainnya",
      amount: String(
        rest.reduce(
          (sum, item) => sum + Number(item.amount || 0),
          0,
        ),
      ),
    });
  }

  return compact.map(item => ({
    ...item,
    share:
      total > 0
        ? Number(item.amount || 0) / total
        : 0,
  }));
}
```

Rename preferred component:

```text
CategoryDonutChart
```

Both Pie and legend must use the same compacted `data`.

---

## C4.2 Analisis category ranking

Analytics should favor exact comparison over another overview donut.

Create:

```text
CategoryRankingChart
```

Use horizontal bars because category names may be long.

Suggested behavior:

```text
descending
top 8 or all if small
amount in tooltip
share in tooltip/label
```

If there are many categories:

```text
Top 8 + Lainnya
```

is acceptable.

## Reference sketch

```jsx
export function CategoryRankingChart({
  items,
  height = 320,
}) {
  const data = compactCategories(items, 8)
    .map(item => ({
      ...item,
      amountValue: Number(item.amount || 0),
    }))
    .sort((a, b) => b.amountValue - a.amountValue);

  if (!data.length) {
    return (
      <p className="empty compact">
        Belum ada pengeluaran terkonfirmasi.
      </p>
    );
  }

  return (
    <div
      className="chart-wrap"
      role="img"
      aria-label="Peringkat kategori pengeluaran"
      style={{ height }}
    >
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={data}
          layout="vertical"
          margin={{ top: 4, right: 12, left: 12, bottom: 4 }}
        >
          <CartesianGrid stroke="#e8ece7" horizontal={false} />
          <XAxis type="number" hide />
          <YAxis
            type="category"
            dataKey="name"
            axisLine={false}
            tickLine={false}
            width={145}
          />
          <Tooltip
            formatter={(value, _, item) => [
              money(String(Math.round(value))),
              `${Math.round(Number(item.payload.share || 0) * 100)}%`,
            ]}
          />
          <Bar
            dataKey="amountValue"
            fill="#5f8f72"
            radius={[0, 4, 4, 0]}
            maxBarSize={24}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
```

## Acceptance tests

```text
dashboard donut <= 6 slices
dashboard legend == donut dataset
Lainnya amount equals omitted categories
shares sum approximately to 1
analytics category chart is horizontal ranking
no category is visually represented without a matching label
```

---

# C5 — Clean up old chart components and page composition

**Priority:** P1 maintainability

After C1–C4, search all usages of:

```text
CashflowChart
DailyCashflowChart
CategoryChart
```

Preferred final set:

```text
DashboardDailySpendingChart
CycleSpendingPatternChart
MonthlyCashflowChart
CategoryDonutChart
CategoryRankingChart
```

If old components have no useful caller, remove them.

Do not keep duplicate implementations "just in case".

## Modification plan

1. Search all web imports.
2. Replace old imports.
3. Delete unused `Area`, `AreaChart`, `Line`, gradient defs if no remaining component uses them.
4. Keep only necessary Recharts imports.
5. Consolidate repeated tooltip styling where practical.
6. Preserve `role="img"` and `aria-label`.
7. Verify responsive CSS.
8. Do not redesign unrelated page sections.

---

# 5. Final page composition

## Ringkasan

Purpose:

```text
current financial position
```

Keep:

```text
active period
Pemasukan
Pengeluaran
Arus kas bersih
Perlu Ditinjau
```

Then:

```text
left:
Pengeluaran harian
compact bar chart

right:
Ke mana uang pergi
Top 5 + Lainnya donut

below:
Transaksi terbaru
```

Do not duplicate income/net in the chart.

## Analisis · Siklus Gaji

Purpose:

```text
spending behavior since payday
```

Cycle KPI row:

```text
Rata-rata / hari
Hari tertinggi
Hari tanpa pengeluaran
Siklus berjalan
```

Hero:

```text
SIKLUS GAJI · HARIAN
Pola pengeluaran siklus ini

daily bars
average reference
```

Lower analysis:

```text
Peringkat kategori
Merchant
Kontribusi anggota
```

The existing `Pengeluaran bulanan setelah refund` card should not appear in
cycle mode because daily cycle data is not a monthly series.

## Analisis · Kalender

Purpose:

```text
month-over-month comparison
```

Calendar KPI row:

```text
Total Pemasukan
Total Pengeluaran
Arus Kas Bersih
Pengembalian Dana
```

Hero:

```text
TREND BULANAN
Pemasukan vs pengeluaran
```

Grouped bar chart.

Lower analysis:

```text
Peringkat kategori
Merchant
Kontribusi anggota
Pengeluaran bulanan setelah refund
```

Recommended: remove the extra `Kecepatan pengeluaran siklus aktif` panel from
calendar mode. Users can switch to `Siklus Gaji` for cycle analysis.

---

# 6. Important data-contract checks before coding

## `/api/v1/analytics/cycle/daily`

Verify whether `daily[]` is:

```text
dense: one row for every elapsed date
or
sparse: only dates with financial activity
```

This matters for zero-spend days and chart continuity.

Do not infer the contract from screenshots.

## Calendar cashflow

Verify rows contain:

```text
period
income
expense
netCashflow
```

for each requested month.

Do not generate monthly data from transaction lists client-side.

## Category values

Verify:

```text
amount
share
name
id
```

Recompute compact shares after grouping `Lainnya`.

---

# 7. Derived metrics implementation preference

Pure calculations should be deterministic functions.

Recommended helpers:

```text
deriveCycleSpendingMetrics
compactCategories
mapMonthlyCashflow
```

Suggested module:

```text
apps/web/app/lib/chartData.js
```

Do not use React effects for pure calculations.

---

# 8. Visual design rules

Keep current Richmod design language.

Use restrained existing colors:

```text
green        income / positive
coral/red    spending
muted blue   neutral reference
soft greens  category distribution
```

Do not use:

```text
3D
dual Y axis
log money axis
excessive gradients
10+ pie slices
animated gimmicks
```

Y axis can remain visually hidden for compact dashboard charts.

For analytics, minimal compact money ticks are acceptable if useful.

Tooltips must use IDR formatting.

Mobile must not clip:

```text
reference-line label
category labels
x-axis labels
```

---

# 9. Tooltip quality

Daily example:

```text
27 Agu 2026

Pengeluaran
Rp 142.000
```

Monthly example:

```text
Agustus 2026

Pemasukan
Rp 16.317.359

Pengeluaran
Rp 382.487

Arus bersih
Rp 15.934.872
```

---

# 10. Testing plan

Run:

```bash
cd apps/web
npm test
npm run build
```

Add pure helper tests where possible.

Suggested file:

```text
apps/web/tests/chart-data.test.mjs
```

Required helper cases:

```text
category compaction:
- 0/1/5/6/10 categories
- Top 5 preserved
- Lainnya correct
- shares correct

cycle metrics:
- average
- zero elapsed
- peak day
- zero-spend days
- all-zero cycle

monthly mapping:
- numeric conversion
- month label
- empty data
```

Update source/product-alignment tests without making them brittle to JSX
whitespace.

---

# 11. Manual visual verification

Inspect:

```text
desktop wide
tablet
mobile
```

Cases:

```text
one day
six days
31-day cycle
zero-spend days
one huge expense day
salary much larger than expenses
more than 6 categories
3 calendar months
12 calendar months
empty state
```

Check:

```text
x-axis ticks
tooltip
reference-line label
legend
card height
category labels
horizontal overflow
```

---

# 12. Performance constraints

No backend/worker redesign is needed.

Frontend:

```text
derive chart data in-memory
avoid redundant network calls
do not fetch transaction-level data for aggregate charts
do not add another chart dependency
```

Arrays of 31 days / 12 months / top categories do not justify complicated
memoization.

---

# 13. Accessibility

Preserve:

```text
role="img"
aria-label
```

Monthly two-series comparison must have legend/tooltip labels so meaning is not
color-only.

---

# 14. Empty states

Use clear text instead of a blank card:

```text
Belum ada pengeluaran pada periode ini.
Belum ada data bulanan pada periode ini.
Belum ada pengeluaran terkonfirmasi.
```

Zero spending is valid data, not an API error.

---

# 15. Out of scope

Do not add:

```text
budgets
budget targets
ideal spending pace
saving targets
arbitrary spending alerts
assets/investment analytics
forecasting
AI chart explanations
new LLM calls
new chart library
new infrastructure
```

The average daily line is descriptive, not prescriptive.

---

# 16. Implementation sequence

```text
1. Read AGENTS.md and current web code.
2. Verify cycle/daily API density contract.
3. Add pure chart-data helpers + tests.
4. Implement C1 dashboard daily-spending chart.
5. Implement C2 cycle spending pattern + KPIs.
6. Implement C3 monthly grouped cashflow + calendar KPIs.
7. Implement C4 compact category donut + ranking.
8. Implement C5 cleanup.
9. Adjust responsive CSS.
10. npm test.
11. npm run build.
12. Manual desktop/mobile check.
13. Inspect diff.
14. Update relevant docs if they describe old chart semantics.
15. Commit + push branch.
16. Merge --no-ff into main + push main.
17. Deploy only if explicitly requested.
```

---

# 17. Definition of Done

## Ringkasan

```text
[ ] Hero no longer plots salary-sized income and daily expenses together.
[ ] Hero is daily spending bars only.
[ ] Income / expense / net stay in KPI cards.
[ ] Period eyebrow is correct for salary-cycle vs calendar fallback.
[ ] Category donut uses Top 5 + Lainnya.
[ ] Donut and legend use the exact same dataset.
```

## Analisis · Siklus Gaji

```text
[ ] Title is "Pola pengeluaran siklus ini".
[ ] Daily expense bars are the hero.
[ ] Cumulative expense line is removed.
[ ] Average-daily reference line is present.
[ ] Average uses elapsed calendar days.
[ ] KPIs are:
    - Rata-rata / hari
    - Hari tertinggi
    - Hari tanpa pengeluaran
    - Siklus berjalan
[ ] Cycle mode does not show misleading "pengeluaran bulanan" derived from daily rows.
[ ] Category analysis uses ranking rather than another crowded donut.
```

## Analisis · Kalender

```text
[ ] Title is "Pemasukan vs pengeluaran".
[ ] Hero uses grouped monthly bars.
[ ] No third net visual series.
[ ] Net remains available via KPI/tooltip.
[ ] 3/6/12 month filters work.
[ ] Custom range works.
[ ] Calendar mode does not unnecessarily duplicate cycle hero analysis.
```

## Components

```text
[ ] Names describe financial meaning.
[ ] Old unused chart components are removed or fully repurposed.
[ ] Unused Recharts imports are removed.
[ ] No second chart dependency is added.
```

## Quality

```text
[ ] Empty states work.
[ ] Mobile works.
[ ] Tooltips format IDR.
[ ] No dual Y axis.
[ ] No log scale.
[ ] No excessive pie slices.
[ ] No budget/ideal-spending semantics introduced.
```

## Verification

```text
[ ] npm test passes.
[ ] npm run build passes.
[ ] Manual desktop check passes.
[ ] Manual mobile check passes.
[ ] Diff has no unrelated backend changes.
[ ] Relevant docs updated if they describe old behavior.
[ ] Branch/worktree workflow follows AGENTS.md.
```

---

# 18. Codex completion report

When finished, report:

```text
baseline main SHA
branch
worktree path
files changed
chart components added/removed
API changes (normally none)
tests + results
build result
responsive manual verification
commit SHA
merge SHA
pushed main SHA
deployment result only if requested
remaining caveats
```

Do not claim completion if the daily-series density contract was ambiguous and
zero-spend metrics were implemented by guesswork.
