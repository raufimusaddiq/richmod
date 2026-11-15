import assert from "node:assert/strict";
import test from "node:test";
import { cycleRangeLabel, inclusiveEnd, windowLabel } from "../app/lib/cycleDates.js";
import { collapseQuality, cycleLabel } from "../app/lib/cycleReview.js";

test("a closed cycle ends the day before its exclusive end", () => {
  assert.equal(inclusiveEnd("2026-09-25"), "2026-09-24");
  assert.equal(cycleRangeLabel({ start: "2026-08-24", end: "2026-09-25" }), "24 Agu 2026 – 24 Sep 2026");
});

test("consecutive closed cycles never overlap", () => {
  const first = cycleRangeLabel({ start: "2026-08-24", end: "2026-09-25" });
  const second = cycleRangeLabel({ start: "2026-09-25", end: "2026-10-23" });
  assert.equal(first.split(" – ")[1], "24 Sep 2026");
  assert.equal(second.split(" – ")[0], "25 Sep 2026");
  assert.equal(second, "25 Sep 2026 – 22 Okt 2026");
});

test("an active cycle reads berjalan and never prints a raw end", () => {
  assert.equal(cycleRangeLabel({ start: "2026-10-23", end: null, active: true }), "23 Okt 2026 – berjalan");
  assert.equal(cycleRangeLabel({ start: "2026-10-23", end: "2026-11-16", active: true }), "23 Okt 2026 – berjalan");
  assert.doesNotMatch(cycleRangeLabel({ start: "2026-10-23", end: "2026-11-23", active: true }), /Nov/);
});

test("analytics and wealth share one formatter", () => {
  const period = { start: "2026-09-25", end: "2026-10-23", state: "CLOSED" };
  assert.equal(cycleLabel(period), cycleRangeLabel({ start: period.start, end: period.end }));
});

test("invalid input degrades to a dash", () => {
  assert.equal(cycleRangeLabel({ start: "", end: "2026-10-23" }), "—");
});

test("the donut window is spelled out", () => {
  assert.equal(windowLabel({ salaryCycles: true, start: "2026-08-24", end: "2026-11-15" }), "3 siklus gaji · 24 Agu 2026 – 15 Nov 2026");
  assert.equal(windowLabel({ salaryCycles: false, start: "2026-09-01", end: "2026-11-30" }), "3 bulan kalender · 1 Sep 2026 – 30 Nov 2026");
  assert.equal(windowLabel({ salaryCycles: true }), "3 siklus gaji");
});

test("one stale-snapshot fact yields one notice", () => {
  const both = [{ kind: "WEALTH_SNAPSHOT_BEFORE_CYCLE" }, { kind: "WEALTH_SNAPSHOT_UNCHANGED" }, { kind: "OPEN_REVIEWS" }];
  assert.deepEqual(collapseQuality(both).map(b => b.kind), ["WEALTH_SNAPSHOT_BEFORE_CYCLE", "OPEN_REVIEWS"]);
  assert.deepEqual(collapseQuality([{ kind: "WEALTH_SNAPSHOT_UNCHANGED" }]).map(b => b.kind), ["WEALTH_SNAPSHOT_UNCHANGED"]);
});
