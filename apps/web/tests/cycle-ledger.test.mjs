import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { adjacentCycles, buildLedger, buildMatrix, paceAxis, paceCeiling, paceCurves, paceMarkerPosition, paceTicks, compactMillions, directionMark, directionText, hasPace, mapPace, paceReferences, signedMillions } from "../app/lib/cycleLedger.js";
import { readReviewSelection, selectionHref } from "../app/lib/cycleReview.js";
import { cycleFacts, stableCycleFacts } from "./fixtures/cycle-review.mjs";
import { globalCss, tree } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("compact amounts are display-only and never invent a value", () => {
  assert.equal(compactMillions("12500000"), "12,5");
  assert.equal(compactMillions("-600000"), "−0,6");
  assert.equal(compactMillions(null), "—");
  assert.equal(compactMillions("abc"), "—");
  assert.equal(signedMillions("4600000"), "+4,6");
  assert.equal(signedMillions("-700000"), "−0,7");
  assert.equal(signedMillions("0"), "0,0");
  assert.equal(signedMillions("40000"), "0,0", "a rounded zero carries no sign");
  assert.equal(signedMillions(null), "—");
});

test("direction is a neutral marker, never a colour", () => {
  assert.equal(directionMark("-1"), "▼");
  assert.equal(directionMark("5"), "▲");
  assert.equal(directionMark("0"), "");
  assert.equal(directionMark("0.00"), "");
  assert.equal(directionMark(null), "");
});

test("history columns carry the served expense and a real bar length", () => {
  const history = [
    { start: "2026-07-26", end: "2026-08-26", state: "CLOSED", expense: "7200000", netCashflow: "5300000" },
    { start: "2026-08-26", end: "2026-09-26", state: "CLOSED", expense: "8600000", netCashflow: "3900000" },
    { start: "2026-09-26", end: null, state: "ACTIVE", expense: "2900000", netCashflow: "9600000" },
  ];
  const { columns } = buildLedger(history, "2026-08-26");
  assert.equal(columns.length, 3);
  assert.equal(columns[1].expense, "8600000", "the served expense is passed through");
  assert.equal(columns[1].expenseLength, 1, "the largest served expense fills the bar");
  assert.ok(Math.abs(columns[0].expenseLength - 7200000 / 8600000) < 1e-9);
  assert.equal(columns[2].running, true);
  assert.equal(columns[2].until, "berjalan");
  assert.equal(columns[1].selected, true);
  assert.equal(buildLedger([], "").columns.length, 0);
});

test("previous and next follow the served newest-first cycle list", () => {
  const cycles = [{ start: "2026-09-26" }, { start: "2026-08-26" }, { start: "2026-07-26" }];
  assert.deepEqual(adjacentCycles(cycles, "2026-09-26"), { older: "2026-08-26", newer: "" });
  assert.deepEqual(adjacentCycles(cycles, "2026-08-26"), { older: "2026-07-26", newer: "2026-09-26" });
  assert.deepEqual(adjacentCycles(cycles, "2026-07-26"), { older: "", newer: "2026-08-26" });
  assert.deepEqual(adjacentCycles(cycles, "unknown"), { older: "", newer: "" });
  assert.deepEqual(adjacentCycles(undefined, "2026-09-26"), { older: "", newer: "" });
});

test("pace references are the served comparison totals, markers for equal-day values", () => {
  const active = paceReferences({ period: { state: "ACTIVE" }, comparison: { expense: { previous: "1700000", median3: "1900000", previousFullCycle: "8600000" } } });
  assert.deepEqual(active.map(item => [item.key, item.shape]), [["previous-same-day", "marker"], ["median-same-day", "marker"], ["previous-full", "line"]]);
  const closed = paceReferences({ period: { state: "CLOSED" }, comparison: { expense: { previous: "7200000", previousFullCycle: "7200000", median3: "7900000" } } });
  assert.deepEqual(closed.map(item => [item.key, item.shape, item.value]), [["previous-full", "line", "7200000"], ["median", "line", "7900000"]]);
  assert.deepEqual(paceReferences({ period: { state: "CLOSED" }, comparison: { expense: { previous: null, previousFullCycle: null, median3: null } } }), []);
  assert.deepEqual(paceReferences({ period: { state: "CLOSED" }, comparison: {} }), []);
});

test("the pace series uses the served running total ", () => {
  const daily = [{ period: "2026-09-29", cumulativeExpense: "100" }, { period: "2026-09-30", cumulativeExpense: "250" }, { period: "2026-10-01", cumulativeExpense: "400" }];
  assert.equal(hasPace(daily), true);
  assert.equal(hasPace([{ period: "2026-09-29" }]), false, "no served total means no chart");
  assert.equal(hasPace([]), false);
  assert.deepEqual(mapPace(daily).map(item => [item.day, item.runningTotal, item.exact]), [[1, 100, "100"], [2, 250, "250"], [3, 400, "400"]]);
});

test("the reviewed cycle survives a Calendar visit", () => {
  const selection = readReviewSelection("view=cycle&cycle=2026-08-26&category=cat-1&review=drivers");
  assert.equal(selectionHref({ ...selection, view: "calendar" }), "/analytics?view=calendar&range=6&cycle=2026-08-26");
  assert.equal(readReviewSelection("view=calendar&range=6&cycle=2026-08-26").cycle, "2026-08-26");
  assert.equal(selectionHref({ ...readReviewSelection("view=calendar&range=6&cycle=2026-08-26"), view: "cycle" }), "/analytics?view=cycle&cycle=2026-08-26");
  assert.equal(selectionHref({ view: "calendar", range: "3", from: "", to: "" }), "/analytics?view=calendar&range=3");
});

test("ledger source: served numbers only, no colour-only direction, accent reserved for interaction", () => {
  const library = text("app/lib/cycleLedger.js");
  assert.doesNotMatch(library, /\.reduce\(|BigInt\(|parseFloat|toFixed/, "the browser formats and sizes; it never derives totals");
  const styles = globalCss();
  assert.ok(styles.includes(':not([data-meeting-step="changes"]) #matrix'), "the matrix belongs to the position and changes meeting steps");
});

test("page wiring: heatmap, stale-while-revalidate, calendar-aware title", () => {
  const page = tree("app/analytics");
  assert.match(page, /<CategoryHeatmap key=\{facts\.period\.start\} facts=\{facts\}/);
  assert.match(page, /facts\.history\?\.length > 0/, "an absent series renders nothing");
  assert.match(page, /\{loading && !facts && <Skeleton/, "the skeleton is only for the first load");
  assert.match(page, /\{facts && !error && <>/);
  assert.match(page, /setFacts\(null\); setError\(/, "a failed load clears facts instead of showing another cycle");
  assert.match(page, /data-stale=\{refreshing \? "true" : undefined\}/);
  assert.match(page, /title=\{selection\.view === "calendar" \? "Analisis kalender" : "Laporan siklus"\}/);
  assert.ok(page.includes("averageDailyText(facts.spendingShape)"), "whole-rupiah daily average comes from the served figure");
  assert.doesNotMatch(page, /Math\.round/, "no rounding of served money in the page");
  assert.doesNotMatch(page, /maximumFractionDigits: 2/);
  assert.match(page, /ari ini belum penuh/);
  const charts = text("app/components/Charts.js");
  assert.match(charts, /export function CyclePaceChart/);
  assert.match(charts, /Total pengeluaran sampai hari ini belum tersedia/, "a missing running total is explained, not silent");
  assert.doesNotMatch(charts, /cumulativeValue|AreaChart/, "pace is a separate single-series line chart");
});

test("synthetic facts mirror the served contract the ledger and pace chart read", () => {
  for (const facts of [cycleFacts(), cycleFacts("2026-08-26"), cycleFacts("2026-09-25"), stableCycleFacts()]) {
    assert.equal(hasPace(facts.daily), true, "every daily row serves cumulativeExpense");
    assert.equal(facts.daily.at(-1).cumulativeExpense, String(facts.daily.reduce((sum, row) => sum + BigInt(row.expense), 0n)));
    assert.ok(facts.history.length >= 5, "history is served oldest first");
    assert.equal(facts.history[0].expenseDelta, null);
    assert.ok(facts.pace.previousFullCycle.length > 0 && facts.pace.median3.length === facts.daily.length, "pace curves are served: the previous cycle in full, the median over the shared days");
    assert.deepEqual(facts.categoryHistory.cycleStarts, facts.history.map(item => item.start));
    facts.history.forEach((item, index) => {
      const listed = facts.categoryHistory.rows.reduce((sum, row) => sum + BigInt(row.amounts[index]), 0n);
      assert.equal(String(listed + BigInt(facts.categoryHistory.other.amounts[index])), item.expense, "categories plus Lainnya reconcile to each cycle's net expense");
    });
    assert.equal(facts.history.at(-1).start, facts.period.start, "the selected cycle is listed");
  }
});

test("matrix source: text cells, keyboard rows, evidence-only buttons, same-day default", () => {
  const component = text("app/analytics/CycleSections.js");
  assert.match(component, /aria-label=\{`Buka bukti \$\{row\.name\}`\}/, "row buttons are named by their action, apart from the category buttons in the detail list");
  assert.match(component, /aria-pressed=\{row\.selected\}/);
  assert.match(component, /compactMillions\(row\.amounts\[index\]\)/, "every cell is text");
  const page = tree("app/analytics");
  assert.match(page, /<CategoryHeatmap key=\{facts\.period\.start\} facts=\{facts\}/);
  assert.match(page, /<details className="review-daily" ref=\{comparisonRef\} open=\{step === "changes" \|\| undefined\}>\s*<summary>Perbandingan lengkap/);
  for (const kept of ["<ComparisonContext", "<ChangesTable", 'className="comparison-bars"', 'className="diverge"', "Perbandingan lengkap"]) assert.ok(page.includes(kept), `${kept} stays available`);
  assert.doesNotMatch(page, /Δ|hari setara|Hari setara/, "plain-language labels, no Greek delta or 'hari setara'");
  assert.match(page, /Selisih vs hari yang sama/);
  assert.match(page, /Hari yang sama, bukan siklus penuh/);
  assert.match(component, /useState\(sameDay \? "SAME_DAY" : "FULL"\)/, "same-day is the default while the cycle runs");
});

test("data marks are neutral or chart-coloured; decreases are hatched, never another hue; accent stays on interaction", () => {
  const styles = globalCss();
  assert.doesNotMatch(styles, /\.change-track i \{[^}]*var\(--accent\)/, "category change bars are data, not interaction");
  assert.doesNotMatch(styles, /\.comparison-track i[^{]*\{[^}]*var\(--accent\)/, "baseline bars are data, not interaction");
  assert.doesNotMatch(styles, /i\[data-negative\] \{ background: var\(--expense\)/, "a decrease is not a good/bad hue");
  assert.match(styles, /i\[data-negative\] \{ background: repeating-linear-gradient/, "a decrease is a hatched neutral bar");
  assert.match(text("app/components/Charts.js"), /dataKey="amountValue" fill="var\(--chart-category-2\)"/, "chart colour comes from the chart ramp");
  const page = tree("app/analytics");
  assert.match(page, /Total sampai hari ini<\/th>/, "the pace chart has a table equivalent");
  assert.match(page, /facts\.merchantRanking\?\.rows\?\.length \|\| 0\} teratas/, "collapsed sections say what is inside");
});

test("the analytics page is a composition shell over named modules", () => {
  const lines = path => text(path).split("\n").length;
  assert.ok(lines("app/analytics/page.js") < 260, "page.js keeps selection, data loading and composition only");
  assert.match(text("app/analytics/shared.js"), /export function SectionTitle/);
  assert.match(text("app/analytics/CycleDashboard.js"), /export function KpiStrip/);
  assert.match(text("app/analytics/CycleDashboard.js"), /export function NoticeStrip/);
  assert.match(text("app/analytics/CalendarReview.js"), /^"use client";/, "the calendar view owns its hooks");
  assert.match(text("app/analytics/CalendarReview.js"), /export function CalendarReview/);
});

test("an active cycle leads with spending so far; a closed cycle with the full-cycle baseline", () => {
  const kpi = text("app/lib/cycleKpi.js");
  assert.match(kpi, /key: "expense", lead: true/, "net spending is the lead cell");
  assert.ok(kpi.includes("active && days ? ` · hari ke-"), "the lead names the elapsed day while active");
  assert.match(kpi, /Arus kas bersih sejauh ini/, "net cashflow stays visible, as a regular metric, while the cycle runs");
  for (const field of ["netCashflow", "income", "expense", "savingsAllocated", "unallocatedSurplus"]) assert.match(kpi, new RegExp(`cashflow\.${field}`), field);
  assert.doesNotMatch(kpi, /Math\.|reduce|BigInt|parseFloat/, "no arithmetic on served money");
});

test("UX audit fixes: toggling, one vocabulary, specific explainers, a way back, the limit up front", () => {
  const page = tree("app/analytics");
  assert.match(page, /const chooseCategory = \(category, element\) =>/, "every category entry point opens the drawer");
  assert.equal(page.split("chooseCategory").length - 1, 5, "ranking, changes, full table and matrix all use it");
  assert.doesNotMatch(page, /Buka semua detail|allOpen/, "the open-all control is gone; sections are few and collapsed by intent");
  assert.match(text("app/analytics/shared.js"), /Tentang \{about \|\| "data ini"\}/);
  for (const about of ["laju pengeluaran", "pengeluaran harian", "tabungan dan kekayaan", "arus kas bulanan", "tabel bulanan", "distribusi kategori", "merchant", "catatan rumah tangga"]) assert.ok(page.includes(`about="${about}"`), `${about} has its own explainer label`);
  assert.match(text("app/analytics/CycleWhere.js"), /aria-label="Tutup rincian kategori"/, "the drawer has a close button; Escape and Back also close it");
  const card = text("app/components/InsightCard.js");
  assert.doesNotMatch(card, /[Aa]nalisis|Menganalisis/, "the feature is called pembahasan everywhere");
  assert.match(card, /Buat pembahasan/);
  assert.match(card, /Dibatasi satu pembahasan per jam/, "the hourly limit is stated before it is hit");
});

test("served pace curves are used whole or not at all and share the day axis", () => {
  assert.deepEqual(paceCurves(undefined), { previous: null, median: null });
  assert.deepEqual(paceCurves({ previousFullCycle: ["1", "2"], median3: null }), { previous: ["1", "2"], median: null });
  assert.equal(paceCurves({ previousFullCycle: ["1", null] }).previous, null, "a partial series is never drawn");
  assert.equal(paceCurves({ previousFullCycle: [] }).previous, null);
  const items = [{ period: "2026-09-01", cumulativeExpense: "100" }, { period: "2026-09-02", cumulativeExpense: "250" }];
  const data = mapPace(items, { previousFullCycle: ["10", "20", "30", "40"], median3: ["5", "15"] });
  assert.equal(data.length, 4, "the previous cycle can run past the selected one");
  assert.deepEqual(data.map(point => [point.day, point.runningTotal, point.previousTotal, point.medianTotal]), [[1, 100, 10, 5], [2, 250, 20, 15], [3, null, 30, null], [4, null, 40, null]]);
  assert.equal(data[2].period, undefined);
  assert.deepEqual(mapPace(items).map(point => [point.previousTotal, point.medianTotal]), [[null, null], [null, null]], "no served curves, none drawn");
  assert.equal(mapPace(items, { previousFullCycle: ["10", "20", "30"] })[1].exact, "250", "exact served strings stay available for the tooltip");
});

test("pace chart source: curves from the served series; levels only when no curve already draws them", () => {
  const charts = text("app/components/Charts.js");
  assert.match(charts, /dataKey="previousTotal"/);
  assert.match(charts, /dataKey="medianTotal"/);
  assert.match(charts, /!\(reference\.tone === "previous" && curves\.previous\)/);
  assert.match(charts, /!\(reference\.tone === "median" && curves\.median\)/);
  assert.match(charts, /reference\.shape === "marker"/, "equal-day markers stay and sit on the curves");
  assert.match(tree("app/analytics"), /pace={pace}/);
  assert.doesNotMatch(charts, /cumulativeValue|AreaChart/, "still a separate single-purpose line chart");
});

test("choosing a category opens its evidence without moving the reader; length and waiting are stated", () => {
  const page = text("app/analytics/page.js");
  assert.doesNotMatch(page, /if \(!selection\.category \|\| step \|\| loading\) return;/, "no effect pulls focus to the evidence after a choice");
  assert.match(text("app/analytics/CycleWhere.js"), /role="dialog" aria-modal="true" aria-labelledby="drivers-title"/, "the choice opens a named modal dialog");
  assert.match(text("app/analytics/CycleWhere.js"), /trapKeyDown\(event, drawer/, "focus trap and Escape come from lib/focusTrap");
  const insight = text("app/components/InsightCard.js");
  assert.match(insight, /tiap 5 detik hingga sekitar 7 menit/);
  assert.match(text("app/lib/insightData.js"), /attempts = 90[^]*wait\(5000, signal\)/, "the stated wait matches the polling budget: 90 x 5s");
});


test("pace x ticks stay at three or four labels on any cycle length", () => {
  for (const length of [6, 28, 30, 31, 45]) {
    const ticks = paceTicks(length);
    assert.ok(ticks.length >= 3 && ticks.length <= 5, `${length} days gives ${ticks.length} ticks`);
    assert.equal(ticks[0], 1);
    assert.ok(ticks.every(day => day <= length));
  }
  assert.deepEqual(paceTicks(9, 4), [1, 5, 9], "an explicit stride is still honoured");
});

test("a reference level that extends the axis gets round ticks, not raw served values", () => {
  const axis = paceAxis(4_300_000);
  assert.deepEqual(axis.domain, [0, 8_000_000], "the top is a round step above the highest level");
  assert.deepEqual(axis.ticks, [0, 2_000_000, 4_000_000, 6_000_000, 8_000_000]);
  assert.deepEqual(paceAxis(0).domain, [0, 1], "no data does not divide by zero");
  assert.deepEqual(paceAxis(null).domain, [0, 1]);
  const small = paceAxis(700_000);
  assert.equal(small.domain[1], 800_000);
  assert.equal(small.ticks.length, 5);
});

test("the pace marker label flips left once the marker is in the right half", () => {
  assert.equal(paceMarkerPosition(6, 30), "insideTopLeft", "an early day keeps the label to the right");
  assert.equal(paceMarkerPosition(15, 30), "insideTopLeft", "the exact middle still fits to the right");
  assert.equal(paceMarkerPosition(16, 30), "insideTopRight", "the right half anchors the label to the left");
  assert.equal(paceMarkerPosition(30, 30), "insideTopRight", "the last day never overflows the svg");
  assert.equal(paceMarkerPosition(6, 6), "insideTopRight", "a short full cycle flips too");
});

test("chart units are on screen and the whole-value list is not a tooltip", () => {
  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  assert.match(charts, /className="chart-unit">Rp juta</, "the category chart names its unit on screen");
  assert.match(charts, /LabelList dataKey="amountLabel"/, "each bar prints its amount");
  assert.doesNotMatch(charts, /rankCategories/, "the chart never sorts money in the browser");
  assert.match(charts, /const data = items\.map\(/, "server order is used as served");
  const calendar = readFileSync(new URL("../app/analytics/CalendarReview.js", import.meta.url), "utf8");
  assert.match(calendar, /Lihat nilai kategori/, "the value list is reachable without a hover");
});

test("no chart is an unnamed focusable surface: accessibilityLayer is off everywhere", () => {
  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  const chartsInFile = charts.match(/<(LineChart|BarChart|AreaChart)\b[^>]*>/g) || [];
  assert.ok(chartsInFile.length >= 5, "the charts are present");
  for (const tag of chartsInFile) assert.match(tag, /accessibilityLayer=\{false\}/, `every chart turns the layer off: ${tag.slice(0, 40)}`);
  for (const component of ["DashboardDailySpendingChart", "CycleSpendingPatternChart", "CyclePaceChart", "MonthlyCashflowChart", "CategoryRankingChart", "NetWorthHistoryChart"]) {
    assert.ok(charts.includes(`export function ${component}`), `${component} exists`);
  }
  // The daily bars stay focusable links, so the drill-down survives without the layer.
  assert.match(charts, /<a href=\{transactionHref\(period/, "daily bars remain links");
  assert.match(charts, /tabIndex=\{0\}/, "and stay in the tab order");
});

test("the phone range controls never clip a native month field", () => {
  const css = globalCss();
  assert.match(css, /\.cycle-review \.custom-range \{ display: grid; grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/, "two fields share a row");
  assert.match(css, /@media \(max-width: 430px\) \{[^}]*\.cycle-review \.custom-range \{ grid-template-columns: minmax\(0, 1fr\)/, "and stack below 430");
  assert.match(css, /\.cycle-review \.custom-range button \{ grid-column: 1 \/ -1; \}/, "the submit sits under the fields");
});





test("the pace axis covers a comparison curve that ends far above the running cycle", () => {
  const items = [
    { period: "2026-09-01", cumulativeExpense: "120000" },
    { period: "2026-09-02", cumulativeExpense: "200000" },
  ];
  // Served curves: the previous cycle runs to 30M over 30 days, the active cycle is on day 2.
  const pace = { previousFullCycle: Array.from({ length: 30 }, (_, index) => String((index + 1) * 1_000_000)), median3: null };
  const data = mapPace(items, pace);
  const ceiling = paceCeiling({ data, last: data.at(-1).exact });
  assert.equal(ceiling, 30_000_000, "the curve's last served point is the ceiling, not the 200k running total");
  const axis = paceAxis(ceiling);
  assert.ok(axis.domain[1] >= 30_000_000, "the axis covers the whole curve");
  assert.deepEqual(axis.ticks, [0, 10_000_000, 20_000_000, 30_000_000, 40_000_000], "and keeps round ticks");
  assert.equal(axis.ticks.every(tick => tick % 10_000_000 === 0), true);
  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  assert.match(charts, /paceCeiling\(\{ data, levels:/, "the chart asks for the ceiling from the plotted data and references");
});
