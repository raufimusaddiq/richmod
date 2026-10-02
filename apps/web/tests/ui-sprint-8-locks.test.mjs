import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("settings no longer shows or requests the processing-status card", () => {
  const settings = text("app/settings/page.js");
  assert.ok(!settings.includes("Status pemrosesan"));
  assert.ok(!settings.includes("operations/status"));
  assert.ok(!settings.includes("operations:"));
  assert.ok(!globalCss().includes("system-metrics"));
  assert.ok(!text("scripts/visual-smoke.mjs").includes("operations/status"));
  // The remaining endpoints keep their order, so each value maps to the field it fills.
  assert.match(settings, /"household\/members", "salary\/sources", "bank-email-listeners", "financial-email-sources", "integrations\/email-ingress"/);
  assert.match(settings, /salarySources: values\[6\] \|\| \[\], listeners: values\[7\] \|\| \[\], financialSources: values\[8\] \|\| \[\], emailIngress: values\[9\]/);
});

test("transaction date filters carry visible labels and fit the card on iOS", () => {
  const page = text("app/transactions/page.js");
  assert.match(page, /<label className="filter-date"><span>Dari tanggal<\/span><input name="from" type="date"/);
  assert.match(page, /<label className="filter-date"><span>Sampai tanggal<\/span><input name="to" type="date"/);
  const css = globalCss();
  assert.match(css, /\.filter-date input \{[^}]*-webkit-appearance: none; appearance: none;/);
  assert.match(css, /\.filter-date input::-webkit-date-and-time-value/);
});

test("stacked phone actions do not keep the desktop gap before a destructive button", () => {
  assert.match(globalCss(), /@media \(max-width: 719px\) \{\s*\.settings-list \.danger, \.member-actions \.danger, \.row-actions \.danger \{ margin-inline-start: 0; \}/);
});

test("the cycle ledger fills the width with few cycles and card headings align with their rows", () => {
  const css = globalCss();
  assert.match(css, /--shown: min\(var\(--visible\), var\(--cols, 6\)\); --col: calc\(\(100cqw - var\(--label\)\) \/ var\(--shown\)\);/);
  assert.match(css, /\.ledger-cards button \{ display: block; width: 100%; min-height: 44px; padding: 0;/);
});
