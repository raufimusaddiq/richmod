import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { auditActionLabel, auditActionText } from "../app/lib/labels.js";
import { createErrorMessage, createFormErrors, dateRangeError, hasActiveFilters, isScopedToTransaction, listSearch, normalizeAmount, transactionsErrorMessage, trapTarget } from "../app/lib/transactionForm.js";

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

test("an ?id= evidence link keeps scoping the ledger to that row", () => {
  assert.equal(listSearch("?id=abc&q=grab"), "?id=abc&q=grab&limit=50");
  assert.equal(listSearch("?limit=20"), "?limit=20");
  assert.equal(isScopedToTransaction("?id=abc"), true);
  assert.equal(isScopedToTransaction("?q=x"), false);
});

test("locale-formatted amounts are normalized, malformed ones rejected", () => {
  assert.equal(normalizeAmount("25.000"), "25000");
  assert.equal(normalizeAmount("1.250.000"), "1250000");
  assert.equal(normalizeAmount("2.5"), "2.5");
  assert.deepEqual(createFormErrors({ type: "EXPENSE", amount: "25.000", description: "Kopi" }), {});
  assert.ok(createFormErrors({ type: "EXPENSE", amount: "2.5", description: "x" }).amount);
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

test("ledger lifecycle audit actions read in Indonesian", () => {
  assert.equal(auditActionText("CONFIRMED"), "Transaksi dikonfirmasi");
  assert.equal(auditActionText("VOIDED"), "Transaksi diabaikan");
  assert.equal(auditActionLabel.VOID, undefined);
});

test("a failed load keeps rows but drops the stale pager cursor", () => {
  const source = readFileSync(new URL("../app/transactions/page.js", import.meta.url), "utf8");
  assert.match(source, /onFailure: \(\) => setNextCursor\(""\)/);
  assert.doesNotMatch(source, /onFailure: \(\) => \{ setItems\(\[\]\)/);
  assert.doesNotMatch(source, /query\.set\("limit", "50"\)/);
  assert.match(source, /hasActiveFilters\(`\?\$\{query\}`\) && <button type="button" className="secondary" onClick=\{resetFilters\}>Hapus filter<\/button>/);
});

import { canEditTransaction, editErrorMessage, editFormErrors, editPayload, editableFields } from "../app/lib/transactionForm.js";

test("edit: only live transactions are editable and transfers lock category and account", () => {
  for (const status of ["PENDING", "CONFIRMED"]) assert.equal(canEditTransaction({ id: "t", status }), true);
  assert.equal(canEditTransaction({ id: "t", status: "NEEDS_REVIEW" }), false);
  assert.equal(canEditTransaction({ id: "t", status: "VOIDED" }), false);
  assert.equal(canEditTransaction({ status: "CONFIRMED" }), false);
  assert.deepEqual(editableFields({ type: "TRANSFER" }), { category: false, account: false, description: true, note: true });
  assert.equal(editableFields({ type: "INCOME" }).category, true);
});

test("edit: validation and payload", () => {
  assert.ok(editFormErrors({ description: "  " }).description);
  assert.ok(editFormErrors({ description: "x".repeat(501) }).description);
  assert.ok(editFormErrors({ description: "ok", note: "x".repeat(2001) }).note);
  assert.deepEqual(editFormErrors({ description: "Kopi", note: "" }), {});
  // NULL description (ingestion) stays blank without error; blanking an existing one errors.
  assert.deepEqual(editFormErrors({ description: "", note: "" }, { description: null }), {});
  assert.ok(editFormErrors({ description: "", note: "" }, { description: "Lama" }).description);
  assert.deepEqual(editPayload({ type: "INCOME", description: null, categoryId: null }, { categoryId: "c1", accountId: "", description: "", note: "" }), { categoryId: "c1" });
  assert.deepEqual(editPayload({ type: "EXPENSE", categoryId: "c0", description: "Lama", note: "n" }, { categoryId: "", accountId: "a1", description: " Kopi ", note: " " }), { categoryId: null, accountId: "a1", description: "Kopi", note: null });
  // Unchanged (possibly deactivated) category/account are never resent.
  assert.deepEqual(editPayload({ type: "EXPENSE", categoryId: "c0", accountId: "a0", description: "Lama", note: null }, { categoryId: "c0", accountId: "a0", description: "Baru", note: "" }), { description: "Baru" });
  assert.deepEqual(editPayload({ type: "EXPENSE", description: "Lama" }, { categoryId: "", accountId: "", description: "Lama", note: "" }), {});
  assert.deepEqual(Object.keys(editPayload({ type: "TRANSFER" }, { categoryId: "c", accountId: "a", description: "x", note: "n" })).sort(), ["description", "note"]);
});

test("edit: errors map to Indonesian and the audit action has a label", () => {
  assert.match(editErrorMessage(409, { code: "REVIEW_OPEN" }), /tinjauan dulu/);
  assert.match(editErrorMessage(409, { code: "STALE_EDIT" }), /sudah berubah/);
  assert.match(editErrorMessage(409, { code: "TRANSACTION_VOIDED" }), /dibatalkan/);
  assert.doesNotMatch(editErrorMessage(400, { code: "INVALID_CATEGORY", error: "categoryId must identify" }), /categoryId/);
  assert.match(editErrorMessage(500), /Coba lagi/);
  assert.equal(auditActionText("UPDATE_DETAILS"), "Detail transaksi diubah");
});

test("edit: drawer offers Ubah and the dialog keeps drawer key handling out", () => {
  assert.match(page, />Ubah<\/button>/);
  assert.match(page, /aria-labelledby="edit-transaction-title"/);
  assert.match(page, /"If-Match": selected\.updatedAt/);
  assert.match(page, /onEditKeyDown[\s\S]{0,80}stopPropagation/);
});
