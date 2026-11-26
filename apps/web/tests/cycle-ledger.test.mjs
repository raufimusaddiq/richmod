import assert from "node:assert/strict";
import test from "node:test";
import { adjacentCycles, buildLedger, buildMatrix, millionTickFormatter, paceAxis, paceCeiling, paceCurves, paceEndLabels, paceMarkerNote, paceTicks, provisionalDays, compactMillions, directionMark, directionText, hasPace, mapPace, paceReferences, signedMillions } from "../app/lib/cycleLedger.js";
import { readReviewSelection, selectionHref } from "../app/lib/cycleReview.js";
import { cycleFacts, stableCycleFacts } from "./fixtures/cycle-review.mjs";

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

test("history columns carry stacked category segments using the shared Where colour map", () => {
  const facts = cycleFacts();
  const colors = new Map(facts.categoryRanking.rows.map((row, index) => [row.id || "uncategorized", `var(--chart-category-${index + 1})`]));
  const order = facts.categoryRanking.rows.map(row => row.id || "uncategorized");
  const { columns } = buildLedger(facts.history, facts.period.start, facts.categoryHistory, { colors, order });
  assert.equal(columns.length, facts.history.length);
  const latest = columns.at(-1);
  assert.deepEqual(latest.segments.map(seg => seg.key), order);
  assert.deepEqual(latest.segments.map(seg => seg.color), order.map((_, index) => `var(--chart-category-${index + 1})`));
  const earlier = columns[0];
  assert.equal(earlier.segments.at(-1).key, "other");
  assert.equal(earlier.segments.at(-1).color, "var(--chart-category-other)");

  const partialColors = new Map([
    [order[0], "var(--chart-category-1)"],
    [order[1], "var(--chart-category-2)"],
    [order[2], "var(--chart-category-other)"],
  ]);
  const folded = buildLedger(facts.history, facts.period.start, facts.categoryHistory, { colors: partialColors, order }).columns.at(-1);
  assert.deepEqual(folded.segments.map(seg => seg.key), [order[0], order[1], "other"], "categories outside top 6 fold into the single Kategori lain segment");
  assert.equal(folded.segments.at(-1).color, "var(--chart-category-other)");
  assert.equal(folded.segments.at(-1).weight, Number(facts.categoryHistory.rows[2].amounts.at(-1)) + Number(facts.categoryHistory.other.amounts.at(-1)));
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

test("the pace marker label is drawn above the plot, so it never overlaps an end-value label", () => {
  for (const day of [1, 15, 28, 30, 31]) {
    assert.equal(paceMarkerNote({ state: "ACTIVE" }, { day }), `Hari ini · hari ke-${day}`);
  }
  assert.equal(paceMarkerNote({ state: "CLOSED" }, { day: 31 }), "Data terakhir · hari ke-31");
});

test("Rp-juta tick labels stay distinct when the tick step is small", () => {
  const small = millionTickFormatter(100_000);
  assert.deepEqual([0, 25_000, 50_000, 75_000, 100_000].map(small), ["0,00", "0,03", "0,05", "0,08", "0,10"]);
  const large = millionTickFormatter(4_000_000);
  assert.deepEqual([0, 1_000_000, 2_000_000, 3_000_000, 4_000_000].map(large), ["0,0", "1,0", "2,0", "3,0", "4,0"]);
  const tiny = millionTickFormatter(20_000);
  assert.deepEqual([0, 5_000, 10_000, 15_000, 20_000].map(tiny), ["0,000", "0,005", "0,010", "0,015", "0,020"]);
  assert.equal(millionTickFormatter(0)(0), "0,0", "an empty axis keeps one decimal");
});

test("the pace axis covers a comparison curve that ends far above the running cycle", () => {
  const items = [
    { period: "2026-09-01", cumulativeExpense: "120000" },
    { period: "2026-09-02", cumulativeExpense: "200000" },
  ];
  const pace = { previousFullCycle: Array.from({ length: 30 }, (_, index) => String((index + 1) * 1_000_000)), median3: null };
  const data = mapPace(items, pace);
  const ceiling = paceCeiling({ data, last: data.at(-1).exact });
  assert.equal(ceiling, 30_000_000, "the curve's last served point is the ceiling, not the 200k running total");
  const axis = paceAxis(ceiling);
  assert.ok(axis.domain[1] >= 30_000_000, "the axis covers the whole curve");
  assert.deepEqual(axis.ticks, [0, 10_000_000, 20_000_000, 30_000_000, 40_000_000], "and keeps round ticks");
  assert.equal(axis.ticks.every(tick => tick % 10_000_000 === 0), true);
});

test("pace end labels emit one label per series end and offset close endpoints without overlap", () => {
  const items = Array.from({ length: 30 }, (_, index) => ({ period: `2026-10-${String(index + 1).padStart(2, "0")}`, cumulativeExpense: String(Math.round((index + 1) * 10_100_000 / 30)) }));
  const previousFullCycle = Array.from({ length: 31 }, (_, index) => (index >= 29 ? "11000000" : String(Math.round((index + 1) * 11_000_000 / 30))));
  const pace = { previousFullCycle, median3: null };
  const data = mapPace(items, pace);
  const curves = paceCurves(pace);
  const last = data[items.length - 1];
  const references = paceReferences({ period: { state: "ACTIVE" }, comparison: { expense: { previous: "11000000", previousFullCycle: "11000000", median3: null } } });
  const axis = paceAxis(paceCeiling({ data, last: last.exact }));
  const labels = paceEndLabels({ data, last, curves, references, axis, plotHeight: 126, minGapPx: 17 });
  assert.deepEqual(labels.map(item => [item.key, item.text]), [["current", "10,1"], ["previous", "11,0"]], "11,0 appears once for the previous series, never duplicated on same-day + tail");
  const pos = item => (1 - item.value / axis.domain[1]) * 126 + item.dy;
  assert.ok(Math.abs(pos(labels[0]) - pos(labels[1])) >= 16, "adjacent series end labels separate vertically");

  const sameEnd = paceEndLabels({
    data: mapPace(items, { previousFullCycle: items.map(() => "11000000"), median3: items.map(() => "10500000") }),
    last,
    curves: { previous: items.map(() => "11000000"), median: items.map(() => "10500000") },
    references: [],
    axis,
    plotHeight: 126,
    minGapPx: 17,
  });
  assert.equal(sameEnd.length, 3, "one label per series when all three curves are present");
  const ys = sameEnd.map(pos).sort((a, b) => a - b);
  assert.ok(ys[1] - ys[0] >= 16 && ys[2] - ys[1] >= 16, "all three right-end labels keep clear vertical separation");
});

test("pace end labels separate close values by at least label height + 4px, stay inside plot, and keep far values unshifted", () => {
  const items = Array.from({ length: 28 }, (_, index) => ({
    period: `2026-10-${String(index + 1).padStart(2, "0")}`,
    cumulativeExpense: String(Math.round((index + 1) * 11_000_000 / 28)),
  }));
  const previousFullCycle = Array.from({ length: 30 }, (_, index) => String(Math.round((index + 1) * 10_100_000 / 30)));
  const data = mapPace(items, { previousFullCycle, median3: null });
  const curves = paceCurves({ previousFullCycle, median3: null });
  const last = data[items.length - 1];
  const axis = paceAxis(paceCeiling({ data, last: last.exact }));
  const plotHeight = 120;
  const close = paceEndLabels({ data, last, curves, references: [], axis, plotHeight });
  assert.deepEqual(
    close.map(item => [item.key, item.text, item.fill]),
    [["current", "11,0", "var(--ink)"], ["previous", "10,1", "var(--chart-axis)"]],
    "close endpoints emit once per series in their own series colours",
  );
  const yOf = item => (1 - item.value / axis.domain[1]) * plotHeight + item.dy;
  assert.ok(close[0].dy < 0 && close[1].dy > 0, "higher value pushes up, lower value pushes down");
  assert.ok(yOf(close[1]) - yOf(close[0]) >= 20, "vertical separation exceeds one 16px label height + 4px gap");
  for (const item of close) {
    assert.ok(yOf(item) >= 0 && yOf(item) <= plotHeight, "close labels stay inside the plot area");
  }

  const farPrevious = Array.from({ length: 30 }, (_, index) => String(Math.round((index + 1) * 3_000_000 / 30)));
  const farData = mapPace(items, { previousFullCycle: farPrevious, median3: null });
  const far = paceEndLabels({
    data: farData,
    last: farData[items.length - 1],
    curves: paceCurves({ previousFullCycle: farPrevious, median3: null }),
    references: [],
    axis,
    plotHeight,
  });
  assert.deepEqual(far.map(item => item.dy), [0, 0], "far values stay unshifted");

  const nearTopItems = items.map((row, index) => (index === items.length - 1 ? { ...row, cumulativeExpense: "19900000" } : row));
  const nearTopPrev = Array.from({ length: 30 }, () => "19600000");
  const topData = mapPace(nearTopItems, { previousFullCycle: nearTopPrev, median3: null });
  const topLabels = paceEndLabels({
    data: topData,
    last: topData[nearTopItems.length - 1],
    curves: paceCurves({ previousFullCycle: nearTopPrev, median3: null }),
    references: [],
    axis: { domain: [0, 20_000_000], ticks: [0, 5_000_000, 10_000_000, 15_000_000, 20_000_000] },
    plotHeight: 100,
  });
  for (const item of topLabels) {
    const y = (1 - item.value / 20_000_000) * 100 + item.dy;
    assert.ok(y >= 0 && y <= 100, `top-clamped label ${item.key} stays inside [0, 100], got ${y}`);
  }
  assert.ok(((1 - topLabels[1].value / 20_000_000) * 100 + topLabels[1].dy) - ((1 - topLabels[0].value / 20_000_000) * 100 + topLabels[0].dy) >= 20);

  const nearBottomItems = items.map((row, index) => (index === items.length - 1 ? { ...row, cumulativeExpense: "100000" } : row));
  const nearBottomPrev = Array.from({ length: 30 }, () => "200000");
  const bottomData = mapPace(nearBottomItems, { previousFullCycle: nearBottomPrev, median3: null });
  const bottomLabels = paceEndLabels({
    data: bottomData,
    last: bottomData[nearBottomItems.length - 1],
    curves: paceCurves({ previousFullCycle: nearBottomPrev, median3: null }),
    references: [],
    axis: { domain: [0, 20_000_000], ticks: [0, 5_000_000, 10_000_000, 15_000_000, 20_000_000] },
    plotHeight: 100,
  });
  for (const item of bottomLabels) {
    const y = (1 - item.value / 20_000_000) * 100 + item.dy;
    assert.ok(y >= 0 && y <= 100, `bottom-clamped label ${item.key} stays inside [0, 100], got ${y}`);
  }
});

test("buildLedger supports SAME_DAY totals and passes through served per-day fields in FULL mode", () => {
  const facts = cycleFacts();
  const sameDay = buildLedger(facts.history, facts.period.start, facts.categoryHistory, null, "SAME_DAY");
  assert.equal(sameDay.mode, "SAME_DAY");
  assert.deepEqual(sameDay.columns.map(col => col.expense), facts.categoryHistory.sameDay.totals);
  assert.equal(sameDay.columns[0].elapsedDays, 6);
  const full = buildLedger(facts.history, facts.period.start, facts.categoryHistory, null, "FULL");
  assert.equal(full.mode, "FULL");
  assert.equal(full.columns[0].days, facts.history[0].days);
  assert.equal(full.columns[0].averageDailyExpenseRounded, facts.history[0].averageDailyExpenseRounded);
});

test("mapPace extends the x-domain to provisional period.end when present", () => {
  assert.equal(provisionalDays({ start: "2026-09-05", end: "2026-10-05" }), 30);
  assert.equal(provisionalDays({ start: "2026-09-05", end: null }), 0);
  const items = [{ period: "2026-09-05", cumulativeExpense: "100" }, { period: "2026-09-06", cumulativeExpense: "250" }];
  const extended = mapPace(items, null, { start: "2026-09-05", end: "2026-10-07" });
  assert.equal(extended.length, 32);
  assert.equal(extended[1].runningTotal, 250);
  assert.equal(extended[2].runningTotal, null);
});
