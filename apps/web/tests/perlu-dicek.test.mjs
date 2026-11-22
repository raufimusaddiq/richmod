import assert from "node:assert/strict";
import test from "node:test";
import { component } from "./support/component.mjs";
import { renderToStaticMarkup } from "react-dom/server";
import React from "react";
import { canUndoResolution, categoryAnswers, inboxView, legacyViewTarget, resolutionSummary, resolvedLine } from "../app/lib/reviewInbox.js";
import { editFormErrors, editPayload, editableFields, editErrorMessage } from "../app/lib/transactionForm.js";


const categories = [
  { id: "c1", name: "Makan", active: true }, { id: "c2", name: "Transport", active: true },
  { id: "c3", name: "Belanja", active: true }, { id: "c4", name: "Tagihan", active: true }, { id: "c5", name: "Lama", active: false },
];

test("old ?view=actions and ?view=email links move to Keluarga > Sumber otomatis", () => {
  assert.equal(legacyViewTarget("actions"), "/household?tab=sumber#tindakan");
  assert.equal(legacyViewTarget("email"), "/household?tab=sumber#email-masuk-terakhir");
  assert.equal(legacyViewTarget("history"), null);
  assert.equal(legacyViewTarget(null), null);
  assert.equal(inboxView("?view=history"), "history");
  assert.equal(inboxView("?view=transactions"), "transactions");
  assert.equal(inboxView(""), "transactions");
});

test("category answers: top three ranked, sering dipakai only on merchant history, the rest behind Kategori lain", () => {
  const item = { suggestedCategories: [
    { id: "c2", name: "Transport", count: 4, reason: "MERCHANT_HISTORY" }, { id: "c1", name: "Makan", count: 2, reason: "MERCHANT_HISTORY" },
    { id: "c3", name: "Belanja", count: 1, reason: "HOUSEHOLD_RECENT" }, { id: "c4", name: "Tagihan", count: 1, reason: "HOUSEHOLD_RECENT" },
  ] };
  const { answers, others } = categoryAnswers(item, categories);
  assert.deepEqual(answers.map(a => a.id), ["c2", "c1", "c3"]);
  assert.deepEqual(answers.map(a => a.often), [true, false, false]);
  assert.deepEqual(others.map(o => o.id), ["c4"], "inactive categories are never offered");
  const fill = categoryAnswers({ suggestedCategories: [{ id: "c1", name: "Makan", count: 5, reason: "HOUSEHOLD_RECENT" }] }, categories);
  assert.equal(fill.answers[0].often, false, "a household-wide fill is not 'sering dipakai'");
  assert.deepEqual(categoryAnswers({}, categories).answers, []);
});

test("an expense card renders ready answers, a Kategori lain disclosure and Ingat untuk", () => {
  const ReviewCards = component("app/components/ReviewCards.js").default;
  const html = renderToStaticMarkup(React.createElement(ReviewCards, {
    items: [{ id: "t1", type: "EXPENSE", amount: "25000", transactionAt: "2026-11-20T07:20:00Z", merchantName: "Grab", sourceType: "BANK_EMAIL", reason: "AMBIGUOUS_CATEGORY", missingFacts: ["category"], allowedActions: ["CONFIRM_REVIEW", "IGNORE"], presentation: "DECISION",
      suggestedCategories: [{ id: "c2", name: "Transport", count: 4, reason: "MERCHANT_HISTORY" }, { id: "c1", name: "Makan", count: 2, reason: "MERCHANT_HISTORY" }] }],
    categories, accounts: [], wealthAccounts: [], busy: {}, action() {},
  }));
  assert.match(html, /Masuk kategori apa\?/);
  assert.match(html, /<button[^>]*class="answer top"[^>]*>Transport<small>sering dipakai<\/small>/);
  assert.match(html, />Makan<\/button>/);
  assert.match(html, /Kategori lain…/);
  assert.match(html, /Ingat untuk Grab/);
  assert.doesNotMatch(html, /<select name="categoryId"/, "the top answers never force a native select");
});

test("a card with no suggestions still offers Kategori lain, and no memory option without a merchant", () => {
  const ReviewCards = component("app/components/ReviewCards.js").default;
  const html = renderToStaticMarkup(React.createElement(ReviewCards, {
    items: [{ id: "t2", type: "EXPENSE", amount: "1000", transactionAt: "2026-11-20T07:20:00Z", description: "Warung", missingFacts: ["category"], allowedActions: ["CONFIRM_REVIEW", "IGNORE"], presentation: "DECISION" }],
    categories, accounts: [], wealthAccounts: [], busy: {}, action() {},
  }));
  assert.match(html, /Kategori lain…/);
  assert.doesNotMatch(html, /sering dipakai/);
});

test("inline confirmation line: label, amount and what happened, with Urungkan calling reopen", () => {
  const item = { id: "t1", merchantName: "Grab", amount: "25000" };
  assert.deepEqual(resolvedLine({ ...item, type: "EXPENSE", reviewItemId: "ri1" }, "confirm", { categoryId: "c2" }, categories), { id: "t1", reopenId: "ri1", transactionId: "t1", canUndo: true, label: "Grab", amount: "25000", summary: "→ Transport" });
  const tx = { id: "t1", type: "EXPENSE", reviewItemId: "ri1" };
  assert.equal(canUndoResolution(tx, "confirm", {}), true);
  assert.equal(canUndoResolution({ ...tx, reviewItemId: undefined }, "confirm", {}), false, "no review item id, no undo");
  assert.equal(canUndoResolution({ ...tx, type: "TRANSFER" }, "confirm", {}), false);
  for (const name of ["merge", "classify-transfer", "reject"]) assert.equal(canUndoResolution(tx, name, {}), false, name);
  const canonical = { id: "ri9", subjectType: "cycle_residual_case" };
  for (const action of ["IGNORE", "LEAVE_UNALLOCATED", "SET_WEALTH_ACCOUNT", "SET_FINANCIAL_EMAIL_ENTITIES"]) assert.equal(canUndoResolution(canonical, "resolve", { action }), true, action);
  for (const action of ["PRIMARY_SALARY", "ORDINARY_INCOME", "ALLOCATE_RETAINED_BALANCE", "MERGE_EXISTING"]) assert.equal(canUndoResolution(canonical, "resolve", { action }), false, action);
  assert.equal(canUndoResolution({ ...canonical, presentation: "NOTICE" }, "resolve", { action: "IGNORE" }), false, "a notice has nothing to undo");
  assert.equal(resolvedLine(canonical, "resolve", { action: "IGNORE" }).transactionId, null);
  assert.equal(resolutionSummary("reject", null, categories), "diabaikan");
  assert.equal(resolutionSummary("classify-transfer", { classification: "OWN_ACCOUNT" }), "rekening sendiri");
  assert.equal(resolutionSummary("resolve", { action: "IGNORE" }), "diabaikan");
});

test("Tempat · Ubah: merchantName is sent only when it changes and only for non-transfers", () => {
  const tx = { id: "x", type: "EXPENSE", merchantName: "QRIS GRAB*1234", counterpartyName: "QRIS GRAB*1234", description: "d", note: null };
  assert.equal(editableFields(tx).place, true);
  assert.equal(editableFields({ type: "TRANSFER" }).place, false);
  assert.deepEqual(editPayload(tx, { merchantName: "  Grab   Ride ", description: "d", note: "" }), { merchantName: "Grab Ride" });
  assert.deepEqual(editPayload(tx, { merchantName: "QRIS GRAB*1234", description: "d", note: "" }), {});
  assert.deepEqual(editPayload(tx, { merchantName: "", description: "d", note: "" }), {}, "an empty place never clears the merchant");
  assert.deepEqual(editPayload({ ...tx, type: "TRANSFER" }, { merchantName: "X", description: "d", note: "" }), {});
  assert.ok(editFormErrors({ merchantName: "a".repeat(161), description: "d" }, tx).merchantName);
  assert.match(editErrorMessage(409, { code: "TRANSFER_FIELD_LOCKED" }), /tempat/);
});
