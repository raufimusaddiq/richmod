import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { adjacentCycles, buildLedger, buildMatrix, compactMillions, directionMark, directionText, hasPace, mapPace, monthMarkers, paceReferences, signedMillions, verdictPairs } from "../app/lib/cycleLedger.js";
import { readReviewSelection, selectionHref } from "../app/lib/cycleReview.js";
import { cycleFacts, stableCycleFacts } from "./fixtures/cycle-review.mjs";
import { globalCss, tree } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

const entry = (start, end, state, income, expense, expenseDelta = null) => ({ start, end, measuredUntil: end ?? "2026-10-03", state, income, grossExpense: expense, refund: "0", expense, netCashflow: String(BigInt(income) - BigInt(expense)), savingsAllocated: "0", expenseDelta });

const history = [
  entry("2026-05-26", "2026-06-26", "CLOSED", "12000000", "8100000"),
  entry("2026-06-26", "2026-07-26", "CLOSED", "12500000", "7900000", "-200000"),
  entry("2026-07-26", "2026-08-26", "CLOSED", "12500000", "7200000", "-700000"),
  entry("2026-08-26", "2026-09-26", "CLOSED", "12500000", "8600000", "1400000"),
  entry("2026-09-26", null, "ACTIVE", "12500000", "2900000", "-5700000"),
];

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

test("direction is a neutral marker plus a signed value, never a colour", () => {
  assert.equal(directionMark("-1"), "▼");
  assert.equal(directionMark("5"), "▲");
  assert.equal(directionMark("0"), "");
  assert.equal(directionMark("0.00"), "");
  assert.equal(directionMark(null), "");
  assert.match(directionText("-1500000"), /^▼ −Rp/);
  assert.match(directionText("1500000"), /^▲ \+Rp/);
  assert.equal(directionText(null), "—");
});

test("ledger columns map served amounts to lengths and mark selection and running cycles", () => {
  const ledger = buildLedger(history, "2026-08-26", "7900000");
  assert.equal(ledger.columns.length, 5);
  assert.equal(ledger.mode, "chart", "four closed cycles are enough for the chart");
  const selected = ledger.columns.find(column => column.selected);
  assert.equal(selected.start, "2026-08-26");
  assert.equal(selected.until, "– 25 Sep");
  assert.equal(selected.delta, "1400000");
  assert.ok(Math.abs(selected.expenseLength - 8600000 / 12500000) < 1e-9);
  assert.equal(selected.incomeLength, 1);
  assert.ok(Math.abs(ledger.medianLength - 7900000 / 12500000) < 1e-9, "median tick shares the bar scale");
  const running = ledger.columns.at(-1);
  assert.equal(running.running, true);
  assert.equal(running.until, "berjalan");
  assert.equal(buildLedger(history, "2026-08-26", null).medianLength, null);
  assert.equal(buildLedger([], "").columns.length, 0);
});

test("fewer than four closed cycles fall back to cards instead of a chart", () => {
  assert.equal(buildLedger(history.slice(-4), "2026-09-26").mode, "cards");
  assert.equal(buildLedger(history.slice(-5), "2026-09-26").mode, "chart");
});

test("previous and next follow the served newest-first cycle list", () => {
  const cycles = [{ start: "2026-09-26" }, { start: "2026-08-26" }, { start: "2026-07-26" }];
  assert.deepEqual(adjacentCycles(cycles, "2026-09-26"), { older: "2026-08-26", newer: "" });
  assert.deepEqual(adjacentCycles(cycles, "2026-08-26"), { older: "2026-07-26", newer: "2026-09-26" });
  assert.deepEqual(adjacentCycles(cycles, "2026-07-26"), { older: "", newer: "2026-08-26" });
  assert.deepEqual(adjacentCycles(cycles, "unknown"), { older: "", newer: "" });
  assert.deepEqual(adjacentCycles(undefined, "2026-09-26"), { older: "", newer: "" });
});

test("the verdict shows served fields as label and value pairs only", () => {
  const closed = { period: { state: "CLOSED" }, spendingShape: { days: 31 }, comparison: { expense: { amount: "8600000", deltaVsPreviousFullCycle: "1400000", relativeDeltaVsPreviousFullCycle: "0.1944", deltaVsMedian3: "700000" } } };
  const pairs = verdictPairs(closed);
  assert.deepEqual(pairs.map(([label]) => label), ["Pengeluaran bersih", "Dibanding siklus sebelumnya", "Dibanding median 3 siklus"]);
  assert.match(pairs[1][1], /^▲ \+Rp.* · 19,4%$/);
  assert.ok(pairs.every(([label, value]) => typeof label === "string" && typeof value === "string" && !/[.!?]$/.test(label)), "no composed sentences");
  closed.comparison.expense.deltaVsPreviousFullCycle = null;
  closed.comparison.expense.deltaVsMedian3 = null;
  assert.deepEqual(verdictPairs(closed).map(([, value]) => value).slice(1), ["Belum ada pembanding"], "missing history is shown as missing");
  const active = { period: { state: "ACTIVE" }, spendingShape: { days: 7 }, comparison: { expense: { amount: "2900000", previous: "1700000", deltaVsPrevious: "1200000", median3: null } } };
  const activePairs = verdictPairs(active);
  assert.equal(activePairs[0][0], "Pengeluaran bersih · hari ke-7");
  assert.equal(activePairs[1][0], "Siklus sebelumnya di hari yang sama");
  assert.equal(activePairs.length, 2, "no median means no median row");
  assert.deepEqual(verdictPairs({ period: { state: "CLOSED" }, comparison: {} }), []);
});

test("pace references are the served comparison totals, markers for equal-day values", () => {
  const active = paceReferences({ period: { state: "ACTIVE" }, comparison: { expense: { previous: "1700000", median3: "1900000", previousFullCycle: "8600000" } } });
  assert.deepEqual(active.map(item => [item.key, item.shape]), [["previous-same-day", "marker"], ["median-same-day", "marker"], ["previous-full", "line"]]);
  const closed = paceReferences({ period: { state: "CLOSED" }, comparison: { expense: { previous: "7200000", previousFullCycle: "7200000", median3: "7900000" } } });
  assert.deepEqual(closed.map(item => [item.key, item.shape, item.value]), [["previous-full", "line", "7200000"], ["median", "line", "7900000"]]);
  assert.deepEqual(paceReferences({ period: { state: "CLOSED" }, comparison: { expense: { previous: null, previousFullCycle: null, median3: null } } }), []);
  assert.deepEqual(paceReferences({ period: { state: "CLOSED" }, comparison: {} }), []);
});

test("the pace series uses the served running total and marks month starts", () => {
  const daily = [{ period: "2026-09-29", cumulativeExpense: "100" }, { period: "2026-09-30", cumulativeExpense: "250" }, { period: "2026-10-01", cumulativeExpense: "400" }];
  assert.equal(hasPace(daily), true);
  assert.equal(hasPace([{ period: "2026-09-29" }]), false, "no served total means no chart");
  assert.equal(hasPace([]), false);
  assert.deepEqual(mapPace(daily).map(item => [item.day, item.runningTotal, item.exact]), [[1, 100, "100"], [2, 250, "250"], [3, 400, "400"]]);
  assert.deepEqual(monthMarkers(daily), [{ day: 3, label: "1 Okt" }]);
  assert.deepEqual(monthMarkers([{ period: "2026-09-01" }, { period: "2026-09-02" }]), [], "the first day is not a crossing");
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
  const component = text("app/components/CycleLedger.js");
  assert.match(component, /directionMark\(column\.delta\)/);
  assert.match(component, /aria-pressed=\{column\.selected\}/);
  assert.match(component, /role="region" aria-label="Perbandingan siklus, geser untuk semua kolom"/);
  assert.match(component, /<caption className="visually-hidden">/);
  const styles = globalCss();
  const block = styles.slice(styles.indexOf("/* Cycle ledger:"), styles.indexOf("/* end cycle ledger */"));
  assert.ok(block.length > 500, "ledger styles present");
  assert.doesNotMatch(block, /var\(--accent|var\(--danger|var\(--income-soft|var\(--expense-soft/, "no accent or good/bad tinting in the ledger");
  assert.match(styles, /\[data-meeting-step\]:not\(\[data-meeting-step="position"\]\):not\(\[data-meeting-step="changes"\]\) > #ledger/, "the ledger belongs to the position and changes meeting steps");
});

test("page wiring: ledger, stale-while-revalidate, calendar-aware title", () => {
  const page = tree("app/analytics");
  assert.match(page, /<CycleLedger history=\{facts\.history\}/);
  assert.match(page, /facts\.history\?\.length > 0/, "an absent series renders nothing");
  assert.match(page, /\{loading && !facts && <Skeleton/, "the skeleton is only for the first load");
  assert.match(page, /\{facts && !error && <>/);
  assert.match(page, /setFacts\(null\); setError\(/, "a failed load clears facts instead of showing another cycle");
  assert.match(page, /data-stale=\{refreshing \? "true" : undefined\}/);
  assert.match(page, /title=\{selection\.view === "calendar" \? "Analisis kalender" : "Laporan siklus"\}/);
  assert.match(page, /money\(String\(Math\.round\(Number\(facts\.spendingShape\.averageDailyExpense\)\)\)\)/, "whole-rupiah daily average");
  assert.doesNotMatch(page, /maximumFractionDigits: 2/);
  assert.match(page, /hari ini belum penuh/);
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
    assert.deepEqual(facts.categoryHistory.cycleStarts, facts.history.map(item => item.start));
    facts.history.forEach((item, index) => {
      const listed = facts.categoryHistory.rows.reduce((sum, row) => sum + BigInt(row.amounts[index]), 0n);
      assert.equal(String(listed + BigInt(facts.categoryHistory.other.amounts[index])), item.expense, "categories plus Lainnya reconcile to each cycle's net expense");
    });
    assert.equal(facts.history.at(-1).start, facts.period.start, "the selected cycle is listed");
  }
});

const matrixHistory = [entry("2026-06-26", "2026-07-26", "CLOSED", "12500000", "7900000"), entry("2026-07-26", "2026-08-26", "CLOSED", "12500000", "7200000"), entry("2026-08-26", null, "ACTIVE", "12500000", "2900000")];
const matrixSeries = {
  cycleStarts: ["2026-06-26", "2026-07-26", "2026-08-26"],
  rows: [{ id: "cat-1", name: "Belanja rumah", amounts: ["3000000", "2000000", "1000000"] }, { id: "", name: "Belum dikategorikan", amounts: ["400000", "0", "-100000"] }],
  other: { amounts: ["4500000", "5200000", "2000000"] },
};

test("the matrix tints each value within its own row and never invents a category", () => {
  const matrix = buildMatrix(matrixSeries, matrixHistory, "cat-1", ["cat-1"]);
  assert.deepEqual(matrix.map(row => row.name), ["Belanja rumah", "Belum dikategorikan", "Lainnya"]);
  assert.deepEqual(matrix[0].tints, [1, 2000000 / 3000000, 1000000 / 3000000]);
  assert.deepEqual(matrix[1].tints, [1, 0, 0], "zero and negative values carry no tint");
  assert.equal(matrix[0].selected, true);
  assert.equal(matrix[0].selectable, true);
  assert.equal(matrix[1].id, "uncategorized", "the empty category id maps to the uncategorized selection");
  assert.equal(matrix[1].selectable, false, "a row without served evidence is not clickable");
  assert.equal(matrix[2].other, true);
  assert.equal(matrix[2].selectable, false, "the remainder opens no evidence");
});

test("a matrix that does not line up with history or has no rows is not drawn", () => {
  assert.deepEqual(buildMatrix({ ...matrixSeries, rows: [] }, matrixHistory), []);
  assert.deepEqual(buildMatrix(undefined, matrixHistory), []);
  assert.deepEqual(buildMatrix({ ...matrixSeries, cycleStarts: ["2026-06-26", "2026-07-26", "2026-09-01"] }, matrixHistory), [], "columns must match the ribbon");
  assert.deepEqual(buildMatrix({ ...matrixSeries, cycleStarts: matrixSeries.cycleStarts.slice(1) }, matrixHistory), []);
  assert.deepEqual(buildMatrix({ ...matrixSeries, other: { amounts: ["1"] } }, matrixHistory), []);
  assert.deepEqual(buildMatrix({ ...matrixSeries, rows: [{ id: "x", name: "X", amounts: ["1"] }] }, matrixHistory), []);
});

test("matrix source: text cells, keyboard rows, evidence-only buttons, detail demoted without removing facts", () => {
  const component = text("app/components/CycleLedger.js");
  assert.match(component, /<th scope="row">\{row\.selectable && onSelectCategory/);
  assert.match(component, /aria-label=\{`Buka bukti \$\{row\.name\}`\}/, "row buttons are named by their action, apart from the category buttons in the detail list");
  assert.match(component, /aria-pressed=\{row\.selected\}/);
  assert.match(component, /\{compactMillions\(row\.amounts\[i\]\)\}/, "every cell is text");
  const page = tree("app/analytics");
  assert.match(page, /categoryHistory=\{facts\.categoryHistory\}/);
  assert.match(page, /<details className="report-disclosure" open=\{step === "changes"\}>\s*<summary><h2 id="changes-title" tabIndex=\{-1\}>Detail perubahan<\/h2>/);
  for (const kept of ["<ComparisonContext", "<ChangesTable", 'className="comparison-bars"', 'className="change-ranking"', "Perbandingan lengkap"]) assert.ok(page.includes(kept), `${kept} stays available`);
  assert.doesNotMatch(page, /Δ|hari setara|Hari setara/, "plain-language labels, no Greek delta or 'hari setara'");
  assert.match(page, /Selisih vs hari yang sama/);
  assert.match(page, /Hari yang sama, bukan siklus penuh/);
});

test("data marks are neutral or chart-coloured; decreases are hatched, never another hue; accent stays on interaction", () => {
  const styles = globalCss();
  assert.doesNotMatch(styles, /\.change-track i \{[^}]*var\(--accent\)/, "category change bars are data, not interaction");
  assert.doesNotMatch(styles, /\.comparison-track i[^{]*\{[^}]*var\(--accent\)/, "baseline bars are data, not interaction");
  assert.doesNotMatch(styles, /i\[data-negative\] \{ background: var\(--expense\)/, "a decrease is not a good/bad hue");
  assert.match(styles, /i\[data-negative\] \{ background: repeating-linear-gradient/, "a decrease is a hatched neutral bar");
  assert.match(styles, /\.change-ranking > li\[data-selected="true"\] \{ background: var\(--butter\); \}/, "selection is butter, as in the ledger");
  assert.doesNotMatch(styles, /Tabel lengkap · geser/, "no permanent scroll caption on tables that do not scroll");
  assert.match(styles, /background-attachment: local, local, scroll, scroll/, "scroll shadows appear only while a table overflows");
  assert.match(text("app/components/Charts.js"), /dataKey="amountValue" fill="var\(--chart-category-2\)"/, "chart colour comes from the chart ramp");
  const page = tree("app/analytics");
  assert.match(page, /Total sampai hari ini<\/th>/, "the pace chart has a table equivalent");
  assert.match(page, /facts\.categoryChanges\.length\} kategori · \{facts\.merchantDrivers\.length\} merchant/, "collapsed sections say what is inside");
});
