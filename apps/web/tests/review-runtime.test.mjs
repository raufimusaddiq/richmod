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
