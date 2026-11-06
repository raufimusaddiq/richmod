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

const CanonicalCard = component("app/components/review/CanonicalCard.js").default;
test("an acknowledge-only review renders as a notice, not a decision", () => {
  const html = renderToStaticMarkup(React.createElement(CanonicalCard, {
    item: { id: "synthetic", reviewType: "FINANCIAL_EMAIL_FACTS", subjectType: "financial_email_observation", presentation: "NOTICE", amount: "100", allowedActions: ["IGNORE"] },
    categories: [], accounts: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, />Mengerti</);
  assert.match(html, /Tidak ada yang perlu diputuskan/);
  assert.doesNotMatch(html, /FINANCIAL_EMAIL_FACTS|FINANCIAL EMAIL FACTS|Abaikan/);
});

test("a bank email review offers bank fact completion, never document reprocessing", () => {
  const html = renderToStaticMarkup(React.createElement(CanonicalCard, {
    item: { id: "synthetic", reviewType: "DOCUMENT_EXTRACTION_LOW_CONFIDENCE", subjectType: "source_event", presentation: "DECISION", amount: "100", allowedActions: ["COMPLETE_BANK_FACTS", "IGNORE"] },
    categories: [], accounts: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, /Lengkapi detail/);
  assert.doesNotMatch(html, /Proses ulang|DOCUMENT_EXTRACTION/);
});

test("a transfer case with no candidates offers a new transfer with bounded purposes", () => {
  const html = renderToStaticMarkup(React.createElement(CanonicalCard, {
    item: { id: "synthetic", reviewType: "TRANSFER_CLASSIFICATION", subjectType: "source_event", presentation: "DECISION", amount: "100", transferCandidates: [], purposeChoices: ["INTERNAL_TRANSFER", "SAVINGS_TRANSFER"], allowedActions: ["CONFIRM_NEW_TRANSFER", "IGNORE"] },
    categories: [], accounts: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, /select name="purpose" required=""/);
  assert.match(html, /Rekening sendiri/);
  assert.match(html, /Ini transfer baru/);
});

test("choosing the wealth account is the single resolving action", () => {
  const html = renderToStaticMarkup(React.createElement(CanonicalCard, {
    item: { id: "synthetic", reviewType: "WEALTH_OBSERVATION_CONFIRMATION", subjectType: "wealth_observation", presentation: "DECISION", amount: "100", institution: "Bank", accountHint: "1234", allowedActions: ["SET_WEALTH_ACCOUNT", "IGNORE"] },
    categories: [], accounts: [], wealthAccounts: [{ id: "w", name: "Tabungan", active: true }], action() {},
  }));
  assert.match(html, /Memilih akun menyelesaikan tinjauan ini/);
  assert.match(html, /Gunakan akun ini/);
  assert.doesNotMatch(html, /opsional/);
});

const ReviewCard = component("app/components/review/ReviewCard.js").default;
test("a review without a printed date offers one-tap date choices", () => {
  const html = renderToStaticMarkup(React.createElement(ReviewCard, {
    item: { id: "synthetic", type: "EXPENSE", reason: "MISSING_TRANSACTION_DATE", amount: "100", transactionAt: "2026-10-03T00:00:00Z", categoryId: "c", missingFacts: ["transaction_at"], dateChoices: [{ key: "TODAY", label: "Hari ini", date: "2026-10-05" }, { key: "YESTERDAY", label: "Kemarin", date: "2026-10-04" }, { key: "MESSAGE_DATE", label: "Tanggal pesan", date: "2026-10-03" }] },
    categories: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, /Hari ini · 2026-10-05/);
  assert.match(html, /Kemarin · 2026-10-04/);
  assert.match(html, /Tanggal pesan · 2026-10-03/);
  assert.match(html, /Tanggal belum diketahui/);
});

test("without a proposed purpose only classified transfers can be merged into", () => {
  const html = renderToStaticMarkup(React.createElement(CanonicalCard, {
    item: { id: "synthetic", reviewType: "TRANSFER_CLASSIFICATION", subjectType: "source_event", presentation: "DECISION", amount: "100", purposeChoices: ["INTERNAL_TRANSFER"], allowedActions: ["MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"], transferCandidates: [{ id: "t", type: "TRANSFER", amount: "100", transactionAt: "2026-10-03T00:00:00Z", description: "Transfer A", mergeable: true }, { id: "u", type: "UNCLASSIFIED", amount: "100", transactionAt: "2026-10-03T00:00:00Z", description: "Transfer B", mergeable: false }] },
    categories: [], accounts: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, /<button type="button">.*?Transfer A/);
  assert.match(html, /<button type="button" disabled="">.*?Transfer B/);
});

test("a merge button is enabled only for a candidate the server marked mergeable", () => {
  // A classified transfer the resolver would still refuse (other account,
  // incompatible Wealth target) arrives with mergeable=false: no button that
  // can only return 400. A case with a proposed purpose shows no purpose
  // choices, and an unmarked (legacy) candidate stays disabled too.
  const html = renderToStaticMarkup(React.createElement(CanonicalCard, {
    item: { id: "synthetic", reviewType: "TRANSFER_CLASSIFICATION", subjectType: "source_event", presentation: "DECISION", amount: "100", proposedPurpose: "INTERNAL_TRANSFER", allowedActions: ["MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "IGNORE"], transferCandidates: [{ id: "a", type: "TRANSFER", amount: "100", transactionAt: "2026-10-03T00:00:00Z", description: "Refused transfer", mergeable: false }, { id: "b", type: "UNCLASSIFIED", amount: "100", transactionAt: "2026-10-03T00:00:00Z", description: "Accepted row", mergeable: true }, { id: "c", type: "TRANSFER", amount: "100", transactionAt: "2026-10-03T00:00:00Z", description: "Unmarked row" }] },
    categories: [], accounts: [], wealthAccounts: [], action() {},
  }));
  assert.match(html, /<button type="button" disabled="">.*?Refused transfer/);
  assert.match(html, /<button type="button">.*?Accepted row/);
  assert.match(html, /<button type="button" disabled="">.*?Unmarked row/);
});

test("a notice wins over the typed card, so no refused button renders", () => {
  const html = renderToStaticMarkup(React.createElement(CanonicalCard, {
    item: { id: "synthetic", reviewType: "WEALTH_OBSERVATION_CONFIRMATION", subjectType: "wealth_observation", presentation: "NOTICE", amount: "100", allowedActions: ["IGNORE"] },
    categories: [], accounts: [], wealthAccounts: [{ id: "w", name: "Tabungan", active: true }], action() {},
  }));
  assert.match(html, />Mengerti</);
  assert.doesNotMatch(html, /Gunakan akun ini/);
});
