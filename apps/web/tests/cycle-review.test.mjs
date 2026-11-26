import assert from "node:assert/strict";
import test from "node:test";
import { cycleLabel, inclusiveEnd, isBlockingQuality, measuredLabel, ratioLabel, readReviewSelection, selectionHref, signedMoney, splitDataQuality, transactionHref } from "../app/lib/cycleReview.js";
import { money } from "../app/lib/format.js";
import { addMonths } from "../app/lib/calendarReview.js";

test("cycle labels use inclusive Jakarta dates and explicit state", () => {
  const period = { start: "2026-08-26", end: "2026-09-25", measuredUntil: "2026-09-25", state: "CLOSED" };
  assert.equal(inclusiveEnd("2026-09-25"), "2026-09-24");
  assert.match(cycleLabel(period), /26 Agu 2026/);
  assert.match(cycleLabel(period), /24 Sep 2026/);
  assert.match(cycleLabel({ ...period, end: null, state: "ACTIVE" }), /berjalan/);
});

test("comparison labels expose the measured prefix, not the whole closed cycle", () => {
	const label = measuredLabel({ start: "2026-08-24", end: "2026-09-25", measuredUntil: "2026-08-31", state: "CLOSED" });
	assert.match(label, /24 Agu 2026/);
	assert.match(label, /30 Agu 2026/);
	assert.doesNotMatch(label, /Sep/);
	assert.equal(measuredLabel(null), "—");
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

test("data quality notices split into blocking banner items and advisory wealth reminders", () => {
  assert.equal(isBlockingQuality("OPEN_REVIEWS"), true);
  assert.equal(isBlockingQuality("UNCATEGORIZED_EXPENSE"), true);
  assert.equal(isBlockingQuality("PROCESSING_INCOMPLETE"), true);
  assert.equal(isBlockingQuality("MISSING_SALARY_ANCHOR"), true);
  assert.equal(isBlockingQuality("MISSING_WEALTH_SNAPSHOT"), false);
  assert.equal(isBlockingQuality("MISSING_PREVIOUS_WEALTH_SNAPSHOT"), false);
  assert.equal(isBlockingQuality("WEALTH_SNAPSHOT_BEFORE_CYCLE"), false);
  assert.equal(isBlockingQuality("WEALTH_ACCOUNT_SET_CHANGED"), false);
  const split = splitDataQuality([{ kind: "OPEN_REVIEWS" }, { kind: "WEALTH_SNAPSHOT_BEFORE_CYCLE" }, { kind: "MISSING_WEALTH_SNAPSHOT" }]);
  assert.deepEqual(split.blocking.map(item => item.kind), ["OPEN_REVIEWS"]);
  assert.deepEqual(split.advisory.map(item => item.kind), ["WEALTH_SNAPSHOT_BEFORE_CYCLE", "MISSING_WEALTH_SNAPSHOT"]);
});

test("period drill-down keeps its existing inclusive query end", () => {
  assert.equal(transactionHref({ start: "2026-08-26", measuredUntil: "2026-09-25" }), "/transactions?from=2026-08-26&to=2026-09-24&status=CONFIRMED&type=SPENDING&cycle=2026-08-26");
  const href = transactionHref({ start: "2026-08-26", measuredUntil: "2026-09-25" }, { categoryId: "cat-1" });
  const query = new URLSearchParams(href.split("?")[1]);
  assert.equal(query.get("from"), "2026-08-26");
  assert.equal(query.get("to"), "2026-09-24");
  assert.equal(query.get("categoryId"), "cat-1");
  assert.equal(query.get("status"), "CONFIRMED");
  assert.equal(href, "/transactions?from=2026-08-26&to=2026-09-24&status=CONFIRMED&type=SPENDING&categoryId=cat-1&cycle=2026-08-26");
});

test("single-day drill uses the same inclusive from/to and preserves cycle and review", () => {
  const period = { start: "2026-08-26", measuredUntil: "2026-09-25", reviewStep: "spending-shape" };
  for (const day of ["2026-08-31", "2026-09-01", "2026-09-24"]) {
    assert.equal(transactionHref(period, { from: day, to: day }), `/transactions?from=${day}&to=${day}&status=CONFIRMED&type=SPENDING&cycle=2026-08-26&review=spending-shape`);
  }
});

test("month drill uses the first and last inclusive dates, retaining the reviewed cycle", () => {
  for (const [month, end] of [["2026-09", "2026-09-30"], ["2026-12", "2026-12-31"], ["2028-02", "2028-02-29"]]) {
    const href = transactionHref({ start: "2026-08-26", measuredUntil: `${addMonths(month, 1)}-01` }, { from: `${month}-01` });
    assert.equal(href, `/transactions?from=${month}-01&to=${end}&status=CONFIRMED&type=SPENDING&cycle=2026-08-26`);
  }
  assert.equal(transactionHref({ start: "", measuredUntil: "2026-10-01" }, { from: "2026-09-01" }), "/transactions?from=2026-09-01&to=2026-09-30&status=CONFIRMED&type=SPENDING&cycle=");
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
});
