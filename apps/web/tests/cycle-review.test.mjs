import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { changeWidth, cycleLabel, inclusiveEnd, ratioLabel, readReviewSelection, selectionHref, signedMoney, transactionHref } from "../app/lib/cycleReview.js";
import { money } from "../app/lib/format.js";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("cycle labels use inclusive Jakarta dates and explicit state", () => {
  const period = { start: "2026-08-26", end: "2026-09-25", measuredUntil: "2026-09-25", state: "CLOSED" };
  assert.equal(inclusiveEnd("2026-09-25"), "2026-09-24");
  assert.match(cycleLabel(period), /26 Agu 2026/);
  assert.match(cycleLabel(period), /24 Sep 2026/);
  assert.match(cycleLabel({ ...period, end: null, state: "ACTIVE" }), /berjalan/);
});

test("currency and ratio formatting never invent a value for null", () => {
  assert.equal(signedMoney(null), "—");
  assert.equal(signedMoney("0"), money("0"));
  assert.match(signedMoney("-1500000"), /^−/);
  assert.match(signedMoney("1500000"), /^\+/);
  assert.equal(ratioLabel(null), "—");
  assert.equal(ratioLabel("0.75"), "75%");
  assert.equal(ratioLabel("0.755"), "75,5%");
});

test("change weight is proportional to the largest absolute delta, not a threshold", () => {
  const items = [{ deltaVsPrevious: "100000" }, { deltaVsPrevious: "-200000" }, { deltaVsPrevious: null, amount: "0" }];
  assert.equal(changeWidth(items[0], items), "50%");
  assert.equal(changeWidth(items[1], items), "100%");
  assert.equal(changeWidth(items[2], items), "0%");
  assert.equal(changeWidth({ amount: "0" }, [{ deltaVsPrevious: "0" }]), "0%");
});

test("drill-down URLs carry deterministic cycle boundaries with exclusive end", () => {
  const href = transactionHref({ start: "2026-08-26", measuredUntil: "2026-09-25" }, { categoryId: "cat-1" });
  const query = new URLSearchParams(href.split("?")[1]);
  assert.equal(query.get("from"), "2026-08-26");
  assert.equal(query.get("to"), "2026-09-24");
  assert.equal(query.get("categoryId"), "cat-1");
  assert.equal(query.get("status"), "CONFIRMED");
});

test("URL state is explicit and round-trips view, cycle, and range", () => {
  assert.deepEqual(readReviewSelection(""), { view: "cycle", cycle: "", range: "6", from: "", to: "", category: "", step: "" });
  assert.equal(readReviewSelection("view=calendar&range=12").range, "12");
  assert.equal(readReviewSelection("view=calendar&range=5").range, "6");
  assert.equal(selectionHref({ view: "cycle", cycle: "2026-08-26", category: "cat-2", range: "6" }), "/analytics?view=cycle&cycle=2026-08-26&category=cat-2");
  assert.equal(selectionHref({ view: "calendar", range: "3", from: "2026-05", to: "2026-07" }), "/analytics?view=calendar&range=3&from=2026-05&to=2026-07");
});

test("meeting step round-trips; ledger preserves step and cycle on return", () => {
  const selection = readReviewSelection("cycle=2026-08-26&review=drivers&category=cat-2");
  assert.equal(selection.step, "drivers");
  assert.equal(selectionHref(selection), "/analytics?view=cycle&cycle=2026-08-26&category=cat-2&review=drivers");
  assert.equal(readReviewSelection("review=unknown").step, "");
  const query = new URLSearchParams(transactionHref({ start: "2026-08-26", measuredUntil: "2026-09-01", reviewStep: "drivers" }).split("?")[1]);
  assert.equal(query.get("review"), "drivers");
  assert.match(text("app/transactions/page.js"), /step: query.get\("review"\)/);
});

test("review page renders deterministic sections, native chart, and AI-disabled path", () => {
  const page = text("app/analytics/page.js");
  for (const section of ["position", "spending-shape", "changes", "drivers", "destinations", "household", "savings-wealth", "quality", "discussion"]) assert.match(page, new RegExp(`id="${section}"`));
  assert.match(page, /analytics\/cycle-review/);
  assert.match(page, /fetch\(`\/api\/v1\/insights\?\$\{query\}/);
  assert.match(page, /cycle_start: cycleStart/);
  assert.match(page, /generate\?\${query}|insights\/generate\?\${query}/);
  assert.match(page, /window\.history\.pushState/);
  assert.match(page, /Suspense/);
  assert.doesNotMatch(page, /dangerouslySetInnerHTML/);
  assert.doesNotMatch(page, /AI says|AI advice|skor kesehatan/i);
  assert.match(page, /Bukan saran keuangan atau keputusan rumah tangga/);
  assert.match(page, /#changes/);
});

test("review page keeps month-end inclusive bounds and no browser-side totals", () => {
  const page = text("app/analytics/page.js");
  assert.match(page, /measuredUntil/);
  assert.doesNotMatch(page, /reduce\(\(sum, item\) => sum \+ Number\(item\.[a-z]+/i);
});

test("cycle explanations use native disclosures without hiding financial facts", () => {
  const page = text("app/analytics/page.js");
  assert.match(page, /<details className="review-explainer"><summary>Tentang data ini<\/summary><p>\{description\}<\/p><\/details>/);
  assert.match(page, /<span>Refund <strong>\{money\(facts.cashflow.refund\)\}<\/strong><\/span>/);
  assert.match(page, /Hari setara, bukan siklus penuh/);
  for (const field of ["netCashflow", "income", "expense", "savingsAllocated", "unallocatedSurplus"]) assert.match(page, new RegExp(`money\\(facts.cashflow.${field}\\)`));
  assert.match(page, /Tentang rekonsiliasi/);
});

test("insight card hides historical advice rows and the quality percentage", () => {
  const card = text("app/components/InsightCard.js");
  const data = text("app/lib/insightData.js");
  assert.doesNotMatch(card, /insightQuality|Kualitas data|skor/i);
  assert.doesNotMatch(card, /✦/);
  assert.match(data, /historical !== true/);
  assert.match(data, /SALARY_CYCLE/);
});

test("review styles keep deterministic chart colours and a single accent", () => {
  const styles = text("app/globals.css");
  assert.match(styles, /\.cycle-review \{ gap: 0; \}/);
  assert.match(styles, /\.cycle-outcome \{ display: grid; grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/);
  assert.match(styles, /\.change-track i \{ display: block; height: 100%; border-radius: 999px; background: var\(--accent\); \}/);
  assert.match(styles, /@media \(max-width: 680px\) \{[\s\S]*?\.cycle-outcome, \.cycle-review \.cycle-outcome \{ grid-template-columns: repeat\(2, minmax\(0, 1fr\)\); \}/);
});

test("scan-first report keeps every comparison and evidence behind native drill-downs", () => {
  const page = text("app/analytics/page.js");
  assert.match(page, /className="comparison-bars" aria-label="Perbandingan pengeluaran bersih"/);
  assert.match(page, /className="change-ranking" aria-label="Perubahan kategori"/);
  assert.match(page, /<details className="review-daily"><summary>Perbandingan lengkap/);
  for (const field of ["previous", "median3", "deltaVsPrevious", "deltaVsMedian3", "relativeDeltaVsPrevious", "relativeDeltaVsMedian3", "contributionToExpenseChange"]) assert.ok(page.includes(`item.${field}`), field);
  assert.match(page, /open={Boolean\(selectedCategory\) \|\| step === "drivers"}/);
  assert.match(page, /open={step === "discussion"}/);
  assert.match(text("app/components/CycleDecisions.js"), /open={expanded \|\| Boolean\(body\)}/);
  assert.match(page, /aria-label="Perbandingan kategori lengkap, geser untuk semua kolom"/);
});
