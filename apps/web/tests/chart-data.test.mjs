import assert from "node:assert/strict";
import test from "node:test";
import { compactCategories, dayLabel, deriveCycleSpendingMetrics, elapsedDaily, mapMonthlyCashflow } from "../app/lib/chartData.js";

const categories = count => Array.from({ length: count }, (_, index) => ({ id: String(index), name: `Kategori ${index}`, amount: String((count - index) * 100) }));

test("compactCategories keeps the top five and groups the remainder", () => {
  assert.deepEqual(compactCategories(categories(0)), []);
  assert.equal(compactCategories(categories(1)).length, 1);
  assert.equal(compactCategories(categories(5)).length, 5);
  assert.equal(compactCategories(categories(6)).length, 6);
  const compact = compactCategories(categories(10));
  assert.deepEqual(compact.slice(0, 5).map(item => item.name), ["Kategori 0", "Kategori 1", "Kategori 2", "Kategori 3", "Kategori 4"]);
  assert.equal(compact.at(-1).name, "Lainnya");
  assert.equal(compact.at(-1).amount, "1500");
  assert.ok(Math.abs(compact.reduce((sum, item) => sum + item.share, 0) - 1) < 0.000001);
});

test("compactCategories excludes non-positive amounts", () => {
  assert.deepEqual(compactCategories([{ name: "Nol", amount: "0" }, { name: "Negatif", amount: "-1" }]), []);
});

test("cycle metrics use elapsed calendar days and find the peak", () => {
  const metrics = deriveCycleSpendingMetrics({ daily: [{ period: "2026-08-27", expense: "100" }, { period: "2026-08-28", expense: "0" }, { period: "2026-08-29", expense: "300" }], spent: "400", daysElapsed: 3, daysTotal: 31 });
  assert.equal(metrics.average, 400 / 3);
  assert.equal(metrics.peak.period, "2026-08-29");
  assert.equal(metrics.peak.expenseValue, 300);
  assert.equal(metrics.zeroSpendDays, 1);
  assert.equal(metrics.daysTotal, 31);
});

test("cycle metrics handle zero elapsed days and all-zero cycles", () => {
  const metrics = deriveCycleSpendingMetrics({ daily: [{ period: "2026-08-29", expense: "0" }], spent: "0", daysElapsed: 0, daysTotal: 1 });
  assert.equal(metrics.average, 0);
  assert.equal(metrics.zeroSpendDays, 0);
  assert.equal(metrics.peak.expenseValue, 0);
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

test("dayLabel includes the complete Indonesian date", () => {
  assert.match(dayLabel("2026-08-29"), /2026/);
  assert.equal(dayLabel(null), "");
});
