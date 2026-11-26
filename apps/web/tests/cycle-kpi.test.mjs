import assert from "node:assert/strict";
import test from "node:test";
import { averageDailyText, baselineFor, deltaTone, deltaView, isZero, kpiCells, shouldShowLastCycleCard } from "../app/lib/cycleKpi.js";
import { cycleFacts, earlyCycleFacts, firstCycleFacts, noSpendingFacts } from "./fixtures/cycle-review.mjs";

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
  assert.equal(isZero("0"), true);
  assert.equal(isZero("-0.00"), true);
  assert.equal(isZero("100"), false);
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

test("four KPI cells: active lead tile carries verdict and same-day + median chips in neutral words", () => {
  const facts = cycleFacts();
  const cells = Object.fromEntries(kpiCells(facts).map(cell => [cell.key, cell]));
  assert.deepEqual(Object.keys(cells), ["expense", "income", "net", "saved"]);
  assert.equal(cells.expense.lead, true);
  assert.match(cells.expense.label, /hari ke-6$/);
  assert.equal(cells.expense.verdict, facts.takeaways.verdict);
  assert.equal(cells.expense.chips.length, 2);
  assert.equal(cells.expense.chips[0].tone, "bad");
  assert.equal(cells.expense.chips[0].mark, "▲");
  assert.match(cells.expense.chips[0].text, /50% vs siklus lalu$/);
  assert.match(cells.expense.chips[1].text, /10,5% vs median 3 siklus$/);
  assert.equal(cells.income.chips, undefined);
  assert.equal(cells.net.label, "Arus kas bersih sejauh ini");
  assert.equal(cells.net.footnote, "belum final");
  assert.match(cells.saved.sub, /^Belum dialokasikan Rp/);
  const source = JSON.stringify(kpiCells(facts));
  assert.doesNotMatch(source, /hemat|boros/);
});

test("closed KPI lead tile shows one full-cycle chip and day-count footnote, and renders cleanly without verdict", () => {
  const facts = cycleFacts("2026-08-26");
  const cells = kpiCells(facts);
  const expense = cells[0];
  assert.equal(expense.chips.length, 1);
  assert.match(expense.chips[0].text, /vs siklus lalu \(penuh\)$/);
  assert.match(expense.footnote, /6 hari · siklus lalu 31 hari · per hari ada di Siklus ke siklus/);
  assert.equal(cells[2].label, "Arus kas bersih");

  delete facts.takeaways.verdict;
  assert.equal(kpiCells(facts)[0].verdict, "");

  facts.comparison = { ...facts.comparison, previous: null, previousFullCycle: null, expense: { ...facts.comparison.expense, deltaVsPreviousFullCycle: null, relativeDeltaVsPreviousFullCycle: null, previousFullCycle: null } };
  const missing = kpiCells(facts)[0];
  assert.deepEqual(missing.chips, []);
  assert.equal(missing.emptyComparison, "Belum ada pembanding");
});

test("days 1-7 close-out card rule: active salary cycle with days <= 7 and previousCycle", () => {
  const early = earlyCycleFacts();
  assert.equal(shouldShowLastCycleCard(early), true);
  assert.equal(shouldShowLastCycleCard({ ...early, spendingShape: { ...early.spendingShape, days: 7 } }), true);
  assert.equal(shouldShowLastCycleCard({ ...early, spendingShape: { ...early.spendingShape, days: 8 } }), false);
  assert.equal(shouldShowLastCycleCard({ ...early, spendingShape: { ...early.spendingShape, days: 0 } }), false);
  assert.equal(shouldShowLastCycleCard({ ...early, previousCycle: null }), false);
  assert.equal(shouldShowLastCycleCard({ ...early, period: { ...early.period, state: "CLOSED" } }), false);
  assert.equal(shouldShowLastCycleCard({ ...early, period: { ...early.period, kind: "CALENDAR_MONTH" } }), false);
});

test("days 1-3 active cycle hides KPI lead delta chips while keeping verdict, and day 4 shows chips", () => {
  const early = earlyCycleFacts();
  for (const days of [1, 2, 3]) {
    const cell = kpiCells({ ...early, spendingShape: { ...early.spendingShape, days } })[0];
    assert.deepEqual(cell.chips, []);
    assert.equal(cell.verdict, early.takeaways.verdict);
    assert.equal(cell.emptyComparison, "");
  }
  const day4 = kpiCells({ ...early, spendingShape: { ...early.spendingShape, days: 4 } })[0];
  assert.equal(day4.chips.length, 1);
  assert.match(day4.chips[0].text, /vs siklus lalu$/);
});

test("the daily average is the served rounded figure, never rounded in the browser", () => {
  assert.equal(averageDailyText({ averageDailyExpense: "700000.40", averageDailyExpenseRounded: "700000" }), "700000");
});

test("a missing served rounded average is null: no Rp 0 row and no reference line", () => {
  assert.equal(averageDailyText({ averageDailyExpense: "700000.40" }), null);
  assert.equal(averageDailyText(undefined), null);
});

test("delta direction is also text for screen readers, beside the aria-hidden glyph", () => {
  assert.equal(deltaView("-750000", { lowerIsBetter: true }).word, "turun");
  assert.equal(deltaView("750000").word, "naik");
  assert.equal(deltaView("0").word, undefined);
  assert.equal(deltaView(null).word, undefined);
});

test("first cycle: the lead comparison is named as missing, never invented", () => {
  const cells = Object.fromEntries(kpiCells(firstCycleFacts()).map(cell => [cell.key, cell]));
  assert.deepEqual(cells.expense.chips, []);
  assert.equal(cells.expense.emptyComparison, "Belum ada pembanding");
});

test("a cycle with no spending: Rp 0 everywhere, and the served delta still decides the tone", () => {
  const cells = Object.fromEntries(kpiCells(noSpendingFacts()).map(cell => [cell.key, cell]));
  assert.match(text(cells.expense.value), /^Rp\s?0$/);
  assert.equal(cells.expense.chips[0].tone, "good", "spending below the previous cycle is green");
  assert.match(text(cells.net.value), /^Rp\s?0$/);
  assert.match(text(cells.saved.sub), /^Belum dialokasikan Rp\s?0$/);
});
