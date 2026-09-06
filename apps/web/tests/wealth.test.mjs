import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
test("wealth UI keeps decimal strings and supports snapshots", () => {
  const wealth = text("app/wealth/page.js");
  assert.match(wealth, /\/api\/v1\/wealth\/summary/);
  assert.match(wealth, /\/api\/v1\/wealth\/snapshots\/latest/);
  assert.match(wealth, /\/api\/v1\/wealth\/history/);
  assert.doesNotMatch(wealth, /fetch\("\/api\/v1\/wealth"\)/);
  assert.match(wealth, /valueIdr/);
  assert.match(wealth, /source: "MANUAL"/);
  assert.match(wealth, /inputMode=\"numeric\"/);
  assert.doesNotMatch(wealth, /parseFloat|Number\(/);
});
test("manual transfer exposes purpose and wealth account", () => {
  const transactions = text("app/transactions/page.js");
  assert.match(transactions, /value=\"TRANSFER\"/);
  assert.match(transactions, /name=\"purpose\"/);
  assert.match(transactions, /relatedWealthAccountId/);
  const inbox = text("app/inbox/page.js");
  assert.match(inbox, /wealth\/cycle-recaps/);
  assert.doesNotMatch(inbox, /wealth\/residuals/);
  assert.match(inbox, /Alokasi belum tersedia/);
  assert.match(text("app/components/AppShell.js"), /\["\/wealth", "Wealth"/);
});
