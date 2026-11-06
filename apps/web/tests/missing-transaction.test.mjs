import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { cycleEntryWindow, missingTransactionHref, withinCycleWindow } from "../app/lib/missingTransaction.js";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("the missing-transaction handoff carries the residual cycle as an inclusive range", () => {
  // The API returns the cycle's Jakarta dates with an exclusive end.
  assert.equal(missingTransactionHref({ route: "CANONICAL_TRANSACTION_FLOW", cycleStart: "2026-08-25", cycleEnd: "2026-09-25" }), "/transactions?record=missing&from=2026-08-25&to=2026-09-24");
  assert.equal(missingTransactionHref({ route: "CANONICAL_TRANSACTION_FLOW" }), "/transactions");
  assert.equal(missingTransactionHref({ cycleStart: "2026-09-25", cycleEnd: "2026-09-25" }), "/transactions");
  assert.equal(missingTransactionHref({ cycleStart: "bad", cycleEnd: "2026-09-25" }), "/transactions");
});

test("manual entry limits and prefills the date to the handed-off cycle", () => {
  const inside = cycleEntryWindow("?record=missing&from=2026-08-25&to=2026-09-24", new Date(2026, 8, 1, 9, 30));
  assert.deepEqual(inside, { from: "2026-08-25", to: "2026-09-24", min: "2026-08-25T00:00", max: "2026-09-24T23:59", value: "2026-09-01T09:30" });
  const after = cycleEntryWindow("?record=missing&from=2026-08-25&to=2026-09-24", new Date(2026, 10, 6, 9, 30));
  assert.equal(after.value, "2026-09-24T12:00");
  assert.equal(cycleEntryWindow("?from=2026-08-25&to=2026-09-24"), null);
  assert.equal(cycleEntryWindow("?record=missing&from=2026-09-24&to=2026-08-25"), null);
  assert.equal(withinCycleWindow("2026-09-24T23:59", inside), true);
  assert.equal(withinCycleWindow("2026-09-25T00:00", inside), false);
  assert.equal(withinCycleWindow("2026-08-24T23:59", inside), false);
  assert.equal(withinCycleWindow("", inside), false);
  assert.equal(withinCycleWindow("", null), true);
});

test("the inbox and the transaction form use the cycle handoff", () => {
  assert.match(text("app/inbox/page.js"), /CANONICAL_TRANSACTION_FLOW"\) \{ window\.location\.assign\(missingTransactionHref\(result\)\)/);
  const page = text("app/transactions/page.js");
  assert.match(page, /cycleEntryWindow\(window\.location\.search\)/);
  assert.match(page, /required=\{Boolean\(missingWindow\)\} min=\{missingWindow\?\.min\} max=\{missingWindow\?\.max\} defaultValue=\{missingWindow\?\.value\}/);
  assert.match(page, /withinCycleWindow\(values\.transactionAt, missingWindow\)/);
});
