import assert from "node:assert/strict";
import test from "node:test";
import { compactCategories, dayLabel, elapsedDaily, mapMonthlyCashflow, rankCategories } from "../app/lib/chartData.js";

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

test("category ranking keeps real category names without synthetic remainder", () => {
  const ranked = rankCategories(categories(10));
  assert.equal(ranked.length, 10);
  assert.equal(ranked.some(item => item.name === "Lainnya"), false);
  assert.ok(Math.abs(ranked.reduce((sum, item) => sum + item.share, 0) - 1) < 0.000001);
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
