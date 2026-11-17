import assert from "node:assert/strict";
import test from "node:test";
import { compactCategories, dayLabel, elapsedDaily, mapMonthlyCashflow, netWorthHistoryRows, netWorthHistorySummary } from "../app/lib/chartData.js";

const categories = count => Array.from({ length: count }, (_, index) => ({ id: String(index), name: `Kategori ${index}`, amount: String((count - index) * 100) }));

test("compactCategories keeps the top five and groups the remainder", () => {
  assert.deepEqual(compactCategories(categories(0)), []);
  assert.equal(compactCategories(categories(1)).length, 1);
  assert.equal(compactCategories(categories(5)).length, 5);
  assert.equal(compactCategories(categories(6)).length, 6);
  const compact = compactCategories(categories(10));
  assert.deepEqual(compact.slice(0, 5).map(item => item.name), ["Kategori 0", "Kategori 1", "Kategori 2", "Kategori 3", "Kategori 4"]);
  assert.equal(compact.at(-1).name, "Kategori lain");
  assert.equal(compact.at(-1).amount, "1500");
  assert.ok(Math.abs(compact.reduce((sum, item) => sum + item.share, 0) - 1) < 0.000001);
});

test("compactCategories excludes non-positive amounts", () => {
  assert.deepEqual(compactCategories([{ name: "Nol", amount: "0" }, { name: "Negatif", amount: "-1" }]), []);
});

test("elapsedDaily hides future cycle dates but preserves older API responses", () => {
  const daily = Array.from({ length: 31 }, (_, index) => ({ period: `2026-08-${String(index + 1).padStart(2, "0")}` }));
  assert.deepEqual(elapsedDaily(daily, 6), daily.slice(0, 6));
  assert.deepEqual(elapsedDaily(daily, 0), []);
  assert.deepEqual(elapsedDaily(daily), daily);
});

test("monthly mapping converts values and creates localized labels", () => {
  const [month] = mapMonthlyCashflow([{ period: "2026-08", income: "1000", expense: "250", netCashflow: "750" }]);
  assert.equal(month.incomeValue, 1000);
  assert.equal(month.expenseValue, 250);
  assert.equal(month.netValue, 750);
  assert.match(month.label, /Agu/);
  assert.deepEqual(mapMonthlyCashflow([]), []);
});

test("monthly mapping marks only the running month as partial", () => {
  const items = [{ period: "2026-08", income: "1", expense: "1", netCashflow: "0" }, { period: "2026-09", income: "1", expense: "1", netCashflow: "0" }];
  assert.deepEqual(mapMonthlyCashflow(items, "2026-09").map(month => month.partial), [false, true]);
  assert.deepEqual(mapMonthlyCashflow(items).map(month => month.partial), [false, false], "no running month means nothing is partial");
});

test("dayLabel includes the complete Indonesian date", () => {
  assert.match(dayLabel("2026-08-29"), /2026/);
  assert.equal(dayLabel(null), "");
});

test("net worth history is oldest first and repeats no date label", () => {
  const rows = netWorthHistoryRows([
    { observedAt: "2026-09-09T15:00:00+07:00", netWorthIdr: "300" },
    { observedAt: "2026-09-09T09:00:00+07:00", netWorthIdr: "200" },
    { observedAt: "2026-09-08T10:00:00+07:00", netWorthIdr: "100" },
  ]);
  assert.deepEqual(rows.map(row => row.value), [100, 200, 300]);
  assert.deepEqual(rows.map(row => row.label), ["8 Sep", "9 Sep", ""]);
  assert.deepEqual(rows.map(row => row.day), ["8 Sep", "9 Sep", "9 Sep"]);
  assert.deepEqual(netWorthHistoryRows([]), []);
});

test("net worth history keeps the same day in different years and shows the year", () => {
  const rows = netWorthHistoryRows([
    { observedAt: "2026-09-09T10:00:00+07:00", netWorthIdr: "200" },
    { observedAt: "2025-09-09T10:00:00+07:00", netWorthIdr: "100" },
  ]);
  assert.deepEqual(rows.map(row => row.label), ["9 Sep 2025", "9 Sep 2026"], "both years keep a label");
  assert.deepEqual(rows.map(row => row.day), ["9 Sep 2025", "9 Sep 2026"]);
  const summary = netWorthHistorySummary(rows);
  assert.match(summary, /9 Sep 2025 sampai 9 Sep 2026/, "the aria range carries both years");
  assert.match(summary, /tertinggi Rp\s200/, "the maximum stays exact");
});

test("net worth history omits the year when every snapshot is in one year", () => {
  const rows = netWorthHistoryRows([
    { observedAt: "2026-09-09T10:00:00+07:00", netWorthIdr: "200" },
    { observedAt: "2026-08-09T10:00:00+07:00", netWorthIdr: "100" },
  ]);
  assert.deepEqual(rows.map(row => row.day), ["9 Agu", "9 Sep"]);
  assert.doesNotMatch(netWorthHistorySummary(rows), /2026/);
});

test("months before the first confirmed transaction are not drawn as blank slots", () => {
  const items = [
    { period: "2026-06", income: "0", expense: "0", refund: "0", netCashflow: "0" },
    { period: "2026-07", income: "0", expense: "0", refund: "0", netCashflow: "0" },
    { period: "2026-08", income: "1000", expense: "250", refund: "0", netCashflow: "750" },
  ];
  assert.deepEqual(mapMonthlyCashflow(items).map(month => month.period), ["2026-08"]);
  assert.deepEqual(mapMonthlyCashflow(items.map(item => ({ ...item, income: "0", expense: "0" }))), [], "no activity at all is an empty chart, not blank slots");
  assert.deepEqual(mapMonthlyCashflow([{ period: "2026-08", income: "0", expense: "0", refund: "250", netCashflow: "-250" }]).map(month => month.period), ["2026-08"], "a refund alone is activity");
});

