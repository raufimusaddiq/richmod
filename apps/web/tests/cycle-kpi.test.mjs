import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";
import { averageDailyText, baselineFor, deltaTone, deltaView, kpiCells } from "../app/lib/cycleKpi.js";
import { cycleFacts, firstCycleFacts, noSpendingFacts } from "./fixtures/cycle-review.mjs";

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

test("four calm KPI cells; only the lead cell carries a comparison, in neutral words (active cycle)", () => {
  const facts = cycleFacts();
  const cells = Object.fromEntries(kpiCells(facts).map(cell => [cell.key, cell]));
  assert.deepEqual(Object.keys(cells), ["expense", "income", "net", "saved"]);
  assert.equal(cells.expense.lead, true);
  assert.match(cells.expense.label, /hari ke-6$/);
  assert.equal(cells.expense.delta.tone, "bad", "spending above the previous cycle's same day is red");
  assert.equal(cells.expense.delta.mark, "▲");
  assert.match(cells.expense.delta.note, /^lebih tinggi vs siklus lalu di hari ke-6$/);
  assert.equal(cells.expense.delta.word, undefined);
  assert.equal(cells.income.delta, undefined);
  assert.equal(cells.net.delta, undefined);
  assert.equal(cells.net.label, "Arus kas bersih sejauh ini");
  assert.equal(cells.net.footnote, "belum final");
  assert.match(cells.saved.sub, /^Belum dialokasikan Rp/);
  const source = JSON.stringify(kpiCells(facts));
  assert.doesNotMatch(source, /hemat|boros/);
});

test("KPI cells for a closed cycle use the full-cycle baseline; a missing comparison is one line of words", () => {
  const facts = cycleFacts("2026-08-26");
  const expense = kpiCells(facts)[0];
  assert.match(expense.delta.note, /^lebih (rendah|tinggi) vs siklus lalu \(penuh\)$/);
  assert.equal(expense.label, "Pengeluaran bersih");
  assert.equal(kpiCells(facts)[2].label, "Arus kas bersih");
  facts.comparison.expense = { ...facts.comparison.expense, deltaVsPreviousFullCycle: null, relativeDeltaVsPreviousFullCycle: null, previousFullCycle: null };
  const missing = kpiCells(facts)[0].delta;
  assert.deepEqual([missing.text, missing.tone, missing.note], ["—", "none", "Belum ada pembanding"]);
});
test("the daily average is the served rounded figure, never rounded in the browser", () => {
  assert.equal(averageDailyText({ averageDailyExpense: "700000.40", averageDailyExpenseRounded: "700000" }), "700000");
});

test("a missing served rounded average is null: no Rp 0 row and no reference line", () => {
  assert.equal(averageDailyText({ averageDailyExpense: "700000.40" }), null);
  assert.equal(averageDailyText(undefined), null);
  const daily = readFileSync(new URL("../app/analytics/CycleWhere.js", import.meta.url), "utf8");
  assert.ok(daily.includes("average != null &&"), "no served average, no Rp 0 legend entry");
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
  const ledger = readFileSync(new URL("../app/lib/cycleLedger.js", import.meta.url), "utf8");
  assert.ok(ledger.includes('period?.state === "CLOSED" ? "Data terakhir" : "Hari ini"'));
  const charts = readFileSync(new URL("../app/components/Charts.js", import.meta.url), "utf8");
  assert.ok(charts.includes('className="pace-marker-note"'));
  assert.ok(readFileSync(new URL("../app/analytics/CycleDashboard.js", import.meta.url), "utf8").includes("period={period}"));
});

test("the category drawer replaces the evidence section and its way back", () => {
  const sections = readFileSync(new URL("../app/analytics/CycleSections.js", import.meta.url), "utf8");
  assert.equal(sections.includes("Kembali ke ringkasan siklus"), false);
  assert.equal(sections.includes('href="#ledger"'), false);
  const history = readFileSync(new URL("../app/analytics/CycleWhere.js", import.meta.url), "utf8");
  assert.ok(history.includes('id="matrix"') && history.includes('id="matrix-title"'), "the target and its heading exist inside Siklus ke siklus");
});

test("first cycle: the lead comparison is named as missing, never invented", () => {
  const cells = Object.fromEntries(kpiCells(firstCycleFacts()).map(cell => [cell.key, cell]));
  assert.equal(cells.expense.delta.empty, true);
  assert.equal(cells.expense.delta.note, "Belum ada pembanding");
  assert.equal(cells.expense.delta.tone, "none");
});
test("a cycle with no spending: Rp 0 everywhere, and the served delta still decides the tone", () => {
  const cells = Object.fromEntries(kpiCells(noSpendingFacts()).map(cell => [cell.key, cell]));
  assert.match(text(cells.expense.value), /^Rp\s?0$/);
  assert.equal(cells.expense.delta.tone, "good", "spending below the previous cycle is green");
  assert.match(cells.expense.delta.note, /^lebih rendah /);
  assert.match(text(cells.net.value), /^Rp\s?0$/);
  assert.match(text(cells.saved.sub), /^Belum dialokasikan Rp\s?0$/);
});
test("a missing comparison is one line of words, not a dash plus a sentence", () => {
  const view = readFileSync(new URL("../app/analytics/CycleDashboard.js", import.meta.url), "utf8");
  assert.ok(view.includes("!delta.empty && <strong>"), "the dash is not drawn when the note already says it is missing");
  assert.ok(view.includes("delta.empty && !delta.note"), "an empty delta with no note renders nothing");
});

test("the pace chart has a keyboard-reachable data table of the same served totals", () => {
  const view = readFileSync(new URL("../app/analytics/CycleDashboard.js", import.meta.url), "utf8");
  assert.ok(view.includes("Lihat nilai laju pengeluaran"), "the disclosure exists");
  assert.ok(view.includes("day.cumulativeExpense"), "the current cycle column is the served cumulative total");
  assert.ok(view.includes("previousCurve[index]") && view.includes("medianCurve[index]"), "the comparison columns are the served curves");
  assert.ok(view.includes('role="region" aria-label="Nilai laju pengeluaran'), "the scrolling table is named");
});
