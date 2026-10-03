import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("settings drops the processing-status card but keeps the data the gateway card reads", () => {
  const settings = text("app/settings/page.js");
  assert.ok(!settings.includes("Status pemrosesan"));
  assert.ok(!globalCss().includes("system-metrics"));
  // The "Worker & LLM gateway" card still reads data.operations, so the request and its mapping must stay.
  assert.match(settings, /data\.operations\?\.worker\?\.healthy/);
  assert.match(settings, /data\.operations\?\.llmGateway\?\.configured/);
  assert.match(settings, /"household\/members", "operations\/status", "salary\/sources", "bank-email-listeners", "financial-email-sources", "integrations\/email-ingress"/);
  assert.match(settings, /members: values\[5\] \|\| \[\], operations: values\[6\], salarySources: values\[7\] \|\| \[\], listeners: values\[8\] \|\| \[\], financialSources: values\[9\] \|\| \[\], emailIngress: values\[10\]/);
  assert.match(settings, /operations: null/);
  // The smoke serves that request, so the gateway card renders its healthy state in the baselines.
  assert.ok(text("scripts/visual-smoke.mjs").includes("/api/v1/operations/status"));
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
  assert.match(globalCss(), /@media \(max-width: 719px\) \{\s*\.settings-list \.danger, \.row-actions \.danger \{ margin-inline-start: 0; \}/);
});

test("the cycle ledger fills the width with few cycles and card headings align with their rows", () => {
  const css = globalCss();
  assert.match(css, /--shown: min\(var\(--visible\), var\(--cols, 6\)\); --col: calc\(\(100cqw - var\(--label\)\) \/ var\(--shown\)\);/);
  assert.match(css, /\.ledger-cards button \{ display: block; width: 100%; min-height: 44px; padding: 0;/);
});
