import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { auditActionLabel, auditActionText } from "../app/lib/labels.js";
import { createErrorMessage, createFormErrors, dateRangeError, hasActiveFilters, listSearch, transactionsErrorMessage, trapTarget } from "../app/lib/transactionForm.js";

const page = readFileSync(new URL("../app/transactions/page.js", import.meta.url), "utf8");

test("focus trap cycles Tab and Shift+Tab inside the dialog", () => {
  const [a, b, c] = ["a", "b", "c"], dialog = "dialog", items = [a, b, c];
  assert.equal(trapTarget(items, c, false, dialog), a, "Tab on the last wraps to the first");
  assert.equal(trapTarget(items, a, true, dialog), c, "Shift+Tab on the first wraps to the last");
  assert.equal(trapTarget(items, dialog, true, dialog), c, "Shift+Tab on the dialog itself goes to the last");
  assert.equal(trapTarget(items, "outside", false, dialog), a, "focus that escaped is pulled back");
  assert.equal(trapTarget(items, b, false, dialog), null, "middle elements move natively");
  assert.equal(trapTarget(items, b, true, dialog), null);
  assert.equal(trapTarget([], "x", false, dialog), dialog, "nothing focusable keeps focus on the dialog");
});

test("an inverted date range is caught before the request, in Indonesian", () => {
  assert.match(dateRangeError("2026-11-10", "2026-01-01"), /tidak boleh setelah/);
  assert.equal(dateRangeError("2026-01-01", "2026-11-10"), "");
  assert.equal(dateRangeError("2026-11-10", "2026-11-10"), "");
  assert.equal(dateRangeError("", "2026-01-01"), "");
  assert.equal(dateRangeError("2026-11-10", ""), "");
  assert.match(page, /dateRangeError\(form\.get\("from"\), form\.get\("to"\)\)/);
});

test("API errors are mapped to Indonesian copy and never shown raw", () => {
  const raw = "from date must not be after to date";
  assert.doesNotMatch(transactionsErrorMessage(400, { error: raw }), /from date/);
  assert.match(transactionsErrorMessage(400, { error: raw }), /Dari/);
  assert.equal(transactionsErrorMessage(400, { error: "limit must be between 1 and 250" }), "Filter tidak valid. Periksa kembali filter lalu coba lagi.");
  assert.equal(transactionsErrorMessage(500, { error: "boom" }), "Transaksi belum dapat dimuat. Coba lagi.");
  assert.doesNotMatch(createErrorMessage(422, { error: "amount must be positive" }), /amount/);
  assert.doesNotMatch(page, /body\.error \|\|/);
});

test("a failed load keeps the visible results and offers a filter reset", () => {
  assert.doesNotMatch(page, /onFailure: \(\) => \{ setItems/);
  assert.match(page, /Hapus filter<\/button>/);
  assert.match(page, /window\.history\.replaceState\([^)]*"\/transactions"\)/);
  assert.equal(hasActiveFilters("?q=grab&limit=50"), true);
  assert.equal(hasActiveFilters("?limit=50&cursor=abc"), false);
});

test("add form validates required fields inline", () => {
  assert.deepEqual(Object.keys(createFormErrors({ type: "EXPENSE", amount: "", description: " " })).sort(), ["amount", "description"]);
  assert.deepEqual(createFormErrors({ type: "EXPENSE", amount: "25000", description: "Kopi" }), {});
  assert.ok(createFormErrors({ type: "EXPENSE", amount: "0", description: "x" }).amount);
  assert.ok(createFormErrors({ type: "TRANSFER", amount: "1", description: "x" }).accountId);
  assert.ok(createFormErrors({ type: "EXPENSE", amount: "1", description: "x" }, { cycle: true }).transactionAt);
});

test("the add dialog has dialog semantics and focus handling", () => {
  assert.match(page, /role="dialog" aria-modal="true" aria-labelledby="create-transaction-title"/);
  assert.match(page, /onKeyDown=\{trapCreateTab\}/);
  assert.match(page, /opener\.focus\(\)/);
  assert.match(page, /className="dialog-close"/);
  assert.match(page, /aria-required="true"/);
});

test("page size label describes a page, not a cap", () => {
  assert.doesNotMatch(page, /Maks\. 250/);
  assert.match(page, /\{PAGE_SIZE\} per halaman/);
});

test("opening ?id= does not narrow the list to one row", () => {
  assert.equal(listSearch("?id=abc&q=grab"), "?q=grab&limit=50");
  assert.equal(listSearch("?id=abc"), "?limit=50");
  assert.equal(listSearch("?limit=20"), "?limit=20");
});

test("audit actions show Indonesian labels with a safe fallback", () => {
  for (const code of ["UPDATE_REVIEW_DETAIL", "RESOLVE_REVIEW", "CREATE_FROM_BANK_EMAIL"]) {
    assert.ok(auditActionLabel[code]);
    assert.notEqual(auditActionText(code), code);
    assert.doesNotMatch(auditActionText(code), /_/);
  }
  assert.equal(auditActionText("SOME_NEW_ACTION"), "Some new action");
  assert.equal(auditActionText(""), "—");
  assert.equal(auditActionText(undefined), "—");
  assert.match(page, /auditActionText\(item\.action\)/);
});
