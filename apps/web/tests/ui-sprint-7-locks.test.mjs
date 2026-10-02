import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";
import { documentTypeLabels } from "../app/lib/labels.js";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("every document type the worker can assign has an Indonesian label", () => {
  const processor = readFileSync(new URL("../../worker/internal/document/processor.go", import.meta.url), "utf8");
  const types = [...new Set(processor.match(/\b(?:RECEIPT|PAYSLIP|BANK_TRANSACTION_SCREENSHOT|TRANSFER_PROOF|EWALLET_SCREENSHOT|BILL_OR_INVOICE|TRANSACTION_HISTORY_SCREENSHOT|WEALTH_OBSERVATION|OTHER_FINANCIAL_DOCUMENT|NON_FINANCIAL_OR_UNSUPPORTED)\b/g) || [])];
  assert.equal(types.length, 10);
  for (const type of types) assert.ok(documentTypeLabels[type], `${type} has no label`);
});

test("settings shows labels, not raw enum values", () => {
  const settings = text("app/settings/page.js");
  for (const raw of ["{item.accountType}", "{item.wealthType}", "{item.side}", "{item.usageRole}", "{item.relationship}", "{item.status}"]) {
    assert.ok(!settings.includes(raw), `settings still renders ${raw}`);
  }
  assert.match(settings, /width: 24, height: 24, minHeight: 24/);
});

test("small text, chart ticks and the dashboard chart follow the 12px floor", () => {
  const css = globalCss();
  assert.match(css, /small \{ font-size: var\(--text-xs\); \}/);
  const charts = text("app/components/Charts.js");
  assert.match(charts, /fontSize: 12 \}/);
  assert.doesNotMatch(charts, /fontSize: 11/);
  assert.match(charts, /padding=\{\{ left: 14, right: 14 \}\}/);
  assert.match(charts, /chart-wrap chart-fill/);
  assert.match(css, /\.chart-fill \{ flex: 1 1 auto; \}/);
});

test("review cards, the upload card and small links keep their layout rules", () => {
  const css = globalCss();
  assert.match(css, /\.review-proposal \{ display: grid;/);
  assert.match(css, /@media \(max-width: 1100px\) \{\s*\.upload-card \{ grid-template-columns: minmax\(0, 1fr\);/);
  assert.match(css, /\.settings-index a \{ min-width: 32px;/);
  assert.match(css, /\.public-legal-links a \{ display: inline-flex; align-items: center; min-height: 32px; \}/);
});
