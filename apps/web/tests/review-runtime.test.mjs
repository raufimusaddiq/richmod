import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import React from "react";
import swc from "next/dist/build/swc/index.js";

const require = createRequire(import.meta.url);
await swc.loadBindings();
const cache = new Map();
function component(filename) {
  const absolute = path.resolve(filename);
  if (cache.has(absolute)) return cache.get(absolute).exports;
  const module = { exports: {} };
  cache.set(absolute, module);
  const { code } = swc.transformSync(fs.readFileSync(absolute, "utf8"), {
    filename: absolute, jsc: { parser: { syntax: "ecmascript", jsx: true }, transform: { react: { runtime: "automatic" } } }, module: { type: "commonjs" },
  });
  new Function("require", "module", "exports", code)(name => name.startsWith(".") ? component(path.resolve(path.dirname(absolute), name.endsWith(".js") ? name : `${name}.js`)) : require(name), module, module.exports);
  return module.exports;
}

const TransferCard = component("app/components/review/TransferCard.js").default;
test("legacy account replacement asks for the missing classification", () => {
  const html = renderToStaticMarkup(React.createElement(TransferCard, {
    item: { id: "synthetic", type: "TRANSFER", missingFacts: ["wealth_account"], knownFacts: {} },
    categories: [], wealthAccounts: [{ id: "replacement", name: "Synthetic replacement", active: true, side: "ASSET", usageRole: "INVESTMENT" }], action() {},
  }));
  assert.match(html, /select name="classification" required=""/);
  assert.match(html, /value="ASSET_PURCHASE"/);
  assert.match(html, /value="INVESTMENT_ACCOUNT"/);
  assert.doesNotMatch(html, /Rekening sendiri|Catat sebagai penghasilan/);
});
for (const classification of ["ASSET_PURCHASE", "INVESTMENT_ACCOUNT"]) {
  test(`${classification} recovery offers active compatible replacements without changing classification`, () => {
    const ReviewCards = component("app/components/ReviewCards.js").default;
    const html = renderToStaticMarkup(React.createElement(ReviewCards, {
      items: [{ id: "synthetic", type: "TRANSFER", missingFacts: ["wealth_account"], knownFacts: { relationship_choice: classification } }],
      categories: [], wealthAccounts: [
        { id: "active", name: "Synthetic replacement", active: true, side: "ASSET", usageRole: "INVESTMENT" },
        { id: "inactive", name: "Synthetic inactive", active: false, side: "ASSET", usageRole: "INVESTMENT" },
        { id: "liability", name: "Synthetic liability", active: true, side: "LIABILITY" },
        { id: "other", name: "Synthetic other", active: true, side: "ASSET", usageRole: "OTHER" },
      ], action() {},
    }));
    assert.match(html, /select name="wealthAccountId" required=""/);
    assert.match(html, /Synthetic replacement/);
    assert.doesNotMatch(html, /Synthetic inactive|Synthetic liability|Rekening sendiri|Catat sebagai penghasilan/);
    if (classification === "INVESTMENT_ACCOUNT") assert.doesNotMatch(html, /Synthetic other/);
  });
}
test("transfer date residual renders a required date field and blocks finalization", () => {
  const html = renderToStaticMarkup(React.createElement(TransferCard, {
    item: { id: "synthetic", type: "UNCLASSIFIED", amount: "100", transactionAt: "2026-10-03", missingFacts: ["transfer_relationship", "transaction_at"] },
    categories: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, /type="date" required=""/);
  assert.match(html, /<button disabled="">Rekening sendiri/);
  assert.match(html, /<button disabled="">Rekening rumah tangga/);
  assert.match(html, /<button class="danger">Abaikan/);
});

test("incoming screenshot review offers income and internal transfers, never outgoing choices", () => {
  const ReviewCards = component("app/components/ReviewCards.js").default;
  const html = renderToStaticMarkup(React.createElement(ReviewCards, {
    items: [{ id: "synthetic", type: "INCOME", reviewType: "TRANSFER_CLASSIFICATION", amount: "100", transactionAt: "2026-10-03", missingFacts: ["transfer_relationship"], allowedActions: ["CLASSIFY_TRANSFER", "OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "IGNORE"] }],
    categories: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, /Rekening sendiri/);
  assert.match(html, /Rekening rumah tangga/);
  assert.match(html, /Catat sebagai penghasilan/);
  assert.doesNotMatch(html, /Investasi \/ RDN|Beli aset|Catat sebagai pengeluaran/);
});
