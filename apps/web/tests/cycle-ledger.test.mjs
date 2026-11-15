import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { adjacentCycles, paceCurves, compactMillions, directionMark, hasPace, mapPace, paceReferences } from "../app/lib/cycleLedger.js";
import { readReviewSelection, selectionHref } from "../app/lib/cycleReview.js";
import { cycleFacts, stableCycleFacts } from "./fixtures/cycle-review.mjs";
import { globalCss, tree } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("compact amounts are display-only and never invent a value", () => {
  assert.equal(compactMillions("12500000"), "12,5");
  assert.equal(compactMillions("-600000"), "−0,6");
  assert.equal(compactMillions(null), "—");
  assert.equal(compactMillions("abc"), "—");
});

test("direction is a neutral marker, never a colour", () => {
  assert.equal(directionMark("-1"), "▼");
  assert.equal(directionMark("5"), "▲");
  assert.equal(directionMark("0"), "");
  assert.equal(directionMark("0.00"), "");
  assert.equal(directionMark(null), "");
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
  assert.match(page, /<CategoryHeatmap facts=\{facts\}/);
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
  assert.match(page, /<CategoryHeatmap facts=\{facts\}/);
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
