import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { buildHeatmap, decisionCount, deltaTone, householdRows, merchantBars, nonZeroAmount } from "../app/lib/cycleSections.js";
import { cycleFacts, stableCycleFacts } from "./fixtures/cycle-review.mjs";
import { globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("same-day heatmap cuts every column at the elapsed day and never tints the running column", () => {
  const facts = cycleFacts();
  const heat = buildHeatmap(facts.categoryHistory, facts.history, "SAME_DAY");
  assert.equal(heat.mode, "SAME_DAY");
  assert.equal(heat.columns.length, facts.history.length);
  assert.ok(heat.columns.every(column => column.subhead.includes("hari 1–")), "each column names its cut");
  assert.ok(heat.columns.every(column => !column.untinted), "same-day columns are comparable");
  assert.equal(heat.rows.at(-1).name, "Kategori lain");
  assert.equal(heat.rows.at(-1).muted, true);
  assert.equal(heat.total.amounts.length, heat.columns.length);
  const full = buildHeatmap(facts.categoryHistory, facts.history, "FULL");
  const running = full.columns.findIndex(column => column.running);
  assert.ok(running >= 0, "the active cycle is a column");
  assert.equal(full.columns[running].untinted, true);
  assert.equal(full.columns[running].subhead, "belum penuh");
  for (const row of full.rows) assert.equal(row.tints[running], 0, "a running column is left out of the tint");
});

test("heatmap totals and deltas are served, never derived", () => {
  const facts = cycleFacts();
  const heat = buildHeatmap(facts.categoryHistory, facts.history, "FULL");
  assert.deepEqual(heat.total.amounts, facts.categoryHistory.totals);
  assert.equal(heat.total.delta, facts.categoryHistory.totalDeltaVsPrevious);
  assert.equal(heat.rows[0].delta, facts.categoryHistory.rows[0].deltaVsPrevious);
  assert.equal(buildHeatmap(undefined, facts.history, "FULL"), null);
  assert.equal(buildHeatmap({ ...facts.categoryHistory, rows: [] }, facts.history, "FULL"), null);
  assert.equal(buildHeatmap({ ...facts.categoryHistory, totals: [] }, facts.history, "FULL"), null);
});

test("merchant bars keep the served order and scale to the largest amount", () => {
  const facts = cycleFacts();
  const list = merchantBars(facts.merchantRanking);
  assert.deepEqual(list.rows.map(row => row.name), facts.merchantRanking.rows.map(row => row.name));
  assert.equal(list.rows[0].length, 1);
  assert.equal(list.compareLabel, "Siklus lalu, hari yang sama");
  assert.equal(merchantBars({ ...facts.merchantRanking, mode: "FULL_CYCLE" }).compareLabel, "Siklus lalu");
  assert.equal(merchantBars(undefined), null);
});

test("household rows keep the server order and use the served share", () => {
  const facts = cycleFacts();
  const rows = householdRows(facts.memberAttribution);
  assert.deepEqual(rows.map(row => row.name), facts.memberAttribution.map(row => row.name));
  assert.equal(rows[0].automatic, true);
  assert.equal(rows[0].initial, "B");
  assert.equal(rows[0].share, 0.4762);
  assert.deepEqual(householdRows([]), []);
});

test("delta tone and the decision counter follow the served sign and code points", () => {
  assert.equal(deltaTone("-1"), "good");
  assert.equal(deltaTone("1"), "bad");
  assert.equal(deltaTone("0"), "none");
  assert.equal(deltaTone(null), "none");
  assert.equal(decisionCount(""), 0);
  assert.equal(decisionCount("abc"), 3);
  assert.equal(decisionCount("😀"), 1, "code points, not UTF-16 units");
});

test("sections source: no money arithmetic, plain model text, fixed order", () => {
  const library = text("app/lib/cycleSections.js");
  assert.doesNotMatch(library, /\.reduce\(|toFixed|parseFloat/, "the browser formats and sizes; it never derives totals");
  const sections = text("app/analytics/CycleSections.js");
  assert.match(sections, /useState\(sameDay \? "SAME_DAY" : "FULL"\)/);
  assert.match(sections, /aria-pressed=\{mode === "SAME_DAY"\}/);
  assert.match(sections, /role="region" aria-label="Pengeluaran per kategori dan siklus/);
  assert.match(sections, /<caption className="visually-hidden">/);
  assert.match(sections, /Urutan tetap: otomatis dulu, lalu anggota menurut nama/);
  assert.match(sections, /<table className="merchant-rank">/, "the merchant ranking is a real table");
  assert.match(sections, /<th scope="col">Merchant<\/th>/, "merchant columns have real headers");
  const card = text("app/components/InsightCard.js");
  assert.doesNotMatch(card, /dangerouslySetInnerHTML|ReactMarkdown/);
  assert.match(card, /Opsional\. Bukan saran keuangan atau keputusan rumah tangga/);
  assert.match(card, /Cek dengan data/);
  const decisions = text("app/components/CycleDecisions.js");
  assert.match(decisions, /\{count\} \/ 2\.000/);
  assert.match(decisions, /decision-columns/);
  const styles = globalCss();
  assert.match(styles, /\.heat-table td\[data-running\] \.heat-cell \{ border: 1px dashed/);
  assert.match(styles, /\.decision-columns \{ display: grid/);
});

test("an empty merchant ranking is not drawn as a zero total", () => {
  const empty = { mode: "FULL_CYCLE", rows: [], other: { amount: "0", count: 0, merchantCount: 0, previous: null, deltaVsPrevious: null }, total: { amount: "0", count: 0, previous: null, deltaVsPrevious: null } };
  assert.deepEqual(merchantBars(empty).rows, []);
  const sections = text("app/analytics/CycleSections.js");
  assert.match(sections, /if \(!list \|\| !list\.rows\.length\) return <p className="empty compact">/, "a zero-row ranking shows the empty state");
  assert.doesNotMatch(sections, /export function MerchantTable/, "the unreferenced merchant table is gone");
});

test("the heatmap resets its mode when the selected cycle changes", () => {
  const page = text("app/analytics/page.js");
  assert.match(page, /<CategoryHeatmap key=\{facts\.period\.start\}/, "a new cycle remounts the heatmap so same-day is re-seeded");
  const sections = text("app/analytics/CycleSections.js");
  assert.match(sections, /useState\(sameDay \? "SAME_DAY" : "FULL"\)/);
  const closed = cycleFacts("2026-08-26");
  assert.equal(buildHeatmap(closed.categoryHistory, closed.history, "FULL").mode, "FULL");
  assert.equal(closed.categoryHistory.sameDay, undefined, "a closed cycle has no same-day matrix");
});

test("the decision form renders even when the list request fails", () => {
  const decisions = text("app/components/CycleDecisions.js");
  const guard = decisions.indexOf('{data && <div className="decision-columns">');
  const guardEnd = decisions.indexOf("</div>}", guard);
  const form = decisions.indexOf("{closed ? <form");
  assert.ok(guard >= 0 && guardEnd > guard && form > guardEnd, "the compose form sits outside the data guard");
  assert.match(decisions, /<form className="decision-form" onSubmit=\{save\}/);
});

test("full-mode tints ignore the untinted running column", () => {
  const history = [
    { start: "2026-07-26", state: "CLOSED" },
    { start: "2026-08-26", state: "CLOSED" },
    { start: "2026-09-26", state: "ACTIVE" },
  ];
  const matrix = {
    cycleStarts: history.map(item => item.start),
    selectedIndex: 2,
    rows: [{ id: "cat-1", name: "Belanja", amounts: ["1000000", "2000000", "9000000"] }],
    other: { amounts: ["0", "0", "0"] },
    totals: ["1000000", "2000000", "9000000"],
  };
  const heat = buildHeatmap(matrix, history, "FULL");
  assert.equal(heat.rows[0].tints[1], 1, "the largest completed column fills the tint");
  assert.equal(heat.rows[0].tints[2], 0, "the running column is never tinted");
  assert.equal(heat.rows[0].tints[0], 0.5, "completed columns keep their relative scale");
});

test("the merchant remainder is only drawn when it has an amount", () => {
  assert.equal(nonZeroAmount("0"), false);
  assert.equal(nonZeroAmount("0.00"), false);
  assert.equal(nonZeroAmount("1"), true);
  assert.equal(nonZeroAmount(null), false);
  const sections = text("app/analytics/CycleSections.js");
  assert.match(sections, /list\.other && nonZeroAmount\(list\.other\.amount\)/, "a zero remainder row is not rendered");
});

test("fixtures carry the served section fields", () => {
  for (const facts of [cycleFacts(), cycleFacts("2026-08-26"), cycleFacts("2026-09-25"), stableCycleFacts()]) {
    assert.ok(facts.merchantRanking && Array.isArray(facts.merchantRanking.rows));
    assert.equal(facts.merchantRanking.total.amount, facts.cashflow.expense, "merchant total equals net expense");
    assert.ok(facts.categoryHistory.totals.length === facts.history.length);
    for (const member of facts.memberAttribution) assert.ok(["AUTOMATIC", "MEMBER"].includes(member.kind));
  }
  const active = cycleFacts();
  assert.ok(active.categoryHistory.sameDay, "an active cycle serves the same-day matrix");
  assert.equal(cycleFacts("2026-08-26").categoryHistory.sameDay, undefined, "a closed cycle has no same-day matrix");
});
