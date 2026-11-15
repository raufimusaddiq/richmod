import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";
import { averageDailyText, baselineFor, deltaTone, deltaView, kpiCells } from "../app/lib/cycleKpi.js";
import { cycleFacts } from "./fixtures/cycle-review.mjs";

const text = value => value.replace(/ /g, " ");

test("delta tone: lower spending is good, higher income is good, zero and null are neutral", () => {
  assert.equal(deltaTone("-750000", true), "good");
  assert.equal(deltaTone("750000", true), "bad");
  assert.equal(deltaTone("750000", false), "good");
  assert.equal(deltaTone("-750000", false), "bad");
  for (const lower of [true, false]) {
    assert.equal(deltaTone("0", lower), "none");
    assert.equal(deltaTone("0.00", lower), "none");
    assert.equal(deltaTone(null, lower), "none");
  }
});

test("delta view: glyph and unsigned magnitude, served percentage, grey dash for a missing comparison, never +0", () => {
  const down = deltaView("-750000", { relative: "-0.0820", lowerIsBetter: true });
  assert.equal(down.mark, "▼");
  assert.equal(down.tone, "good");
  assert.equal(text(down.text), "Rp 750.000");
  assert.equal(down.percent, "(−8,2%)");
  const up = deltaView("310000", { relative: null, lowerIsBetter: true });
  assert.deepEqual([up.mark, up.tone, up.percent], ["▲", "bad", ""], "no served percentage, none invented");
  const zero = deltaView("0");
  assert.deepEqual([zero.mark, zero.tone, zero.zero], ["", "none", true]);
  assert.doesNotMatch(zero.text, /\+/);
  const missing = deltaView(null, { relative: "0.5" });
  assert.deepEqual([missing.text, missing.tone, missing.mark, missing.percent], ["—", "none", "", ""]);
});

test("baseline selection: an active cycle uses the same elapsed day, a closed cycle the previous full cycle", () => {
  const active = baselineFor({ state: "ACTIVE" }, 24);
  assert.deepEqual([active.delta, active.relative, active.field], ["deltaVsPrevious", "relativeDeltaVsPrevious", "previous"]);
  assert.equal(active.label, "vs siklus lalu di hari ke-24");
  const closed = baselineFor({ state: "CLOSED" }, 30);
  assert.deepEqual([closed.delta, closed.relative, closed.field], ["deltaVsPreviousFullCycle", "relativeDeltaVsPreviousFullCycle", "previousFullCycle"]);
  assert.equal(closed.label, "vs siklus lalu (penuh)");
});

test("KPI cells name their baseline and use served fields only (active cycle)", () => {
  const facts = cycleFacts();
  facts.comparison.income = { amount: "12000000", previous: "12000000", deltaVsPrevious: "0", relativeDeltaVsPrevious: "0.0000", previousFullCycle: "12000000", deltaVsPreviousFullCycle: "0" };
  facts.comparison.netCashflow = { amount: "7800000", previous: "7000000", deltaVsPrevious: "800000", relativeDeltaVsPrevious: "0.1143", previousFullCycle: "9000000", deltaVsPreviousFullCycle: "-1200000" };
  const cells = Object.fromEntries(kpiCells(facts).map(cell => [cell.key, cell]));
  assert.deepEqual(Object.keys(cells), ["expense", "income", "net", "unallocated", "worth"]);
  assert.equal(cells.expense.lead, true);
  assert.match(cells.expense.label, /hari ke-6$/);
  assert.equal(cells.expense.delta.tone, "bad", "spending above the previous cycle's same day is red");
  assert.match(cells.expense.delta.note, /^vs siklus lalu di hari ke-6 \(Rp/);
  assert.equal(cells.income.delta.tone, "none");
  assert.match(cells.income.delta.note, /^sama dengan siklus lalu di hari yang sama$/);
  assert.equal(cells.net.delta.tone, "good", "higher net cashflow is green");
  assert.equal(cells.net.footnote, "belum final");
  assert.match(cells.unallocated.sub, /^Ditabung Rp/);
  assert.match(cells.worth.observed, /^Pengamatan /);
});

test("KPI cells for a closed cycle use the full-cycle baseline; a missing comparison is a grey dash", () => {
  const facts = cycleFacts("2026-08-26");
  const expense = kpiCells(facts)[0];
  assert.equal(expense.delta.note.startsWith("vs siklus lalu (penuh)"), true);
  assert.equal(expense.label, "Pengeluaran bersih");
  const cells = Object.fromEntries(kpiCells(facts).map(cell => [cell.key, cell]));
  assert.equal(cells.income.delta.text, "—", "comparison.income absent: no invented delta");
  assert.equal(cells.income.delta.tone, "none");
  assert.equal(cells.income.delta.note, "Belum ada pembanding");
  facts.comparison.expense = { ...facts.comparison.expense, deltaVsPreviousFullCycle: null, relativeDeltaVsPreviousFullCycle: null, previousFullCycle: null };
  const missing = kpiCells(facts)[0].delta;
  assert.deepEqual([missing.text, missing.tone, missing.note], ["—", "none", "Belum ada pembanding"]);
});

test("net-worth cell: latest observation with its date; no in-cycle change is a dash, not a delta", () => {
  const facts = cycleFacts();
  const withChange = kpiCells(facts).find(cell => cell.key === "worth");
  assert.equal(withChange.delta.tone, "good");
  assert.match(withChange.delta.note, /^vs pengamatan /);
  facts.wealth.current = null; facts.wealth.netWorthChange = null;
  const before = kpiCells(facts).find(cell => cell.key === "worth");
  assert.equal(text(before.value), "Rp 40.000.000", "the newest earlier observation is the latest observation");
  assert.equal(before.delta.text, "—");
  assert.match(before.observed, /^Pengamatan terakhir /);
  facts.wealth.previous = null;
  const none = kpiCells(facts).find(cell => cell.key === "worth");
  assert.deepEqual([none.value, none.observed], ["—", "Belum ada pengamatan"]);
});

test("the daily average is the served rounded figure, never rounded in the browser", () => {
  assert.equal(averageDailyText({ averageDailyExpense: "700000.40", averageDailyExpenseRounded: "700000" }), "700000");
});

test("a missing served rounded average is null: no Rp 0 row and no reference line", () => {
  assert.equal(averageDailyText({ averageDailyExpense: "700000.40" }), null);
  assert.equal(averageDailyText(undefined), null);
  const page = readFileSync(new URL("../app/analytics/page.js", import.meta.url), "utf8");
  assert.ok(page.includes('averageDailyText(facts.spendingShape) == null ? "—"'), "the metric row shows a dash");
  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  assert.ok(charts.includes("suppliedAverage == null ? Number.NaN"), "no average, no reference line");
});

test("delta direction is also text for screen readers, beside the aria-hidden glyph", () => {
  assert.equal(deltaView("-750000", { lowerIsBetter: true }).word, "turun");
  assert.equal(deltaView("750000").word, "naik");
  assert.equal(deltaView("0").word, undefined);
  assert.equal(deltaView(null).word, undefined);
  const view = readFileSync(new URL("../app/analytics/CycleDashboard.js", import.meta.url), "utf8");
  assert.ok(view.includes('aria-hidden="true">{delta.mark} </span>'));
  assert.ok(view.includes('className="visually-hidden">{delta.word} </span>'));
});

test("the pace marker says today only while the cycle runs", () => {
  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  assert.ok(charts.includes('period?.state === "CLOSED" ? "Data terakhir" : "Hari ini"'));
  assert.ok(readFileSync(new URL("../app/analytics/CycleDashboard.js", import.meta.url), "utf8").includes("period={period}"));
});
