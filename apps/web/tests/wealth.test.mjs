import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
test("wealth UI keeps decimal strings and supports snapshots", () => {
  const wealth = text("app/wealth/page.js");
  const charts = text("app/components/Charts.js");
  assert.match(wealth, /\/api\/v1\/wealth\/summary/);
  assert.match(wealth, /\/api\/v1\/wealth\/snapshots\/latest/);
  assert.match(wealth, /\/api\/v1\/wealth\/history/);
  assert.doesNotMatch(wealth, /fetch\("\/api\/v1\/wealth"\)/);
  assert.match(wealth, /valueIdr/);
  assert.match(wealth, /source: "MANUAL"/);
  assert.match(wealth, /inputMode=\"numeric\"/);
  assert.doesNotMatch(wealth, /parseFloat|Number\(/);
  assert.match(wealth, /Kekayaan belum diatur/);
  assert.match(wealth, /jakartaLocalToRFC3339/);
  assert.match(wealth, /defaultValue=\{previousValues\.get/);
  assert.match(wealth, /Perbarui nilai/);
  assert.match(charts, /contentStyle=\{defaultTooltipStyle\}/);
});

test("Jakarta datetime-local serialization is explicit", () => {
  const wealth = text("app/wealth/page.js");
  assert.match(wealth, /timeZone: "Asia\/Jakarta"/);
  assert.match(wealth, /\+07:00/);
});
test("homepage reads canonical Net Worth and distinguishes uninitialized Wealth", () => {
  const home = text("app/page.js");
  assert.match(home, /\/api\/v1\/wealth\/snapshots\/latest/);
  assert.match(home, /latestWealth\.netWorthIdr/);
  assert.match(home, /Kekayaan belum diinisialisasi/);
  assert.match(home, /: "—"/);
});
test("Wealth navigation uses a distinct Vault icon", () => {
  const shell = text("app/components/AppShell.js");
  assert.match(shell, /wealth: Vault/);
  assert.match(shell, /\["\/wealth", "Kekayaan", "wealth"\]/);
});
test("manual transfer exposes purpose and wealth account", () => {
  const transactions = text("app/transactions/page.js");
  assert.match(transactions, /value=\"TRANSFER\"/);
  assert.match(transactions, /name=\"purpose\"/);
  assert.match(transactions, /relatedWealthAccountId/);
  const inbox = text("app/inbox/page.js");
  assert.match(inbox, /api\/v1\/reviews/);
  assert.match(text("app/components/ReviewCards.js"), /Alokasikan saldo tersisa/);
  assert.match(text("app/components/ReviewCards.js"), /ALLOCATE_RETAINED_BALANCE/);
  assert.match(text("app/components/ReviewCards.js"), /ASSET_PURCHASE/);
  assert.match(text("app/components/ReviewCards.js"), /Beli aset/);
  assert.match(text("app/components/AppShell.js"), /\["\/wealth", "Kekayaan"/);
});
