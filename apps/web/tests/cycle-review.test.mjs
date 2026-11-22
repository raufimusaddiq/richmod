import assert from "node:assert/strict";
import test from "node:test";
import { changeWidth, cycleLabel, inclusiveEnd, measuredLabel, ratioLabel, readReviewSelection, selectionHref, signedMoney, transactionHref } from "../app/lib/cycleReview.js";
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

test("change weight is proportional to the largest absolute delta, not a threshold", () => {
  const items = [{ deltaVsPrevious: "100000" }, { deltaVsPrevious: "-200000" }, { deltaVsPrevious: null, amount: "0" }];
  assert.equal(changeWidth(items[0], items), "50%");
  assert.equal(changeWidth(items[1], items), "100%");
  assert.equal(changeWidth(items[2], items), "0%");
  assert.equal(changeWidth({ amount: "0" }, [{ deltaVsPrevious: "0" }]), "0%");
});

test("rent compares full cycles by default, not a zero equal-day prefix", () => {
  const rent = { amount: "1950000", previous: "0", deltaVsPrevious: "1950000", previousFullCycle: "1950000", deltaVsPreviousFullCycle: "0", relativeDeltaVsPreviousFullCycle: "0.0000" };
  assert.equal(changeWidth(rent, [rent], "deltaVsPreviousFullCycle"), "0%");
  assert.equal(ratioLabel(rent.relativeDeltaVsPreviousFullCycle), "0%");
  assert.equal(signedMoney(rent.deltaVsPreviousFullCycle), money("0"));
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
