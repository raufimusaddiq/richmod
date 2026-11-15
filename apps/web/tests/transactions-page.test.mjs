import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { auditActionLabel, auditActionText } from "../app/lib/labels.js";
import { VOID_CONFIRM_MESSAGE, canVoidTransaction, mergeSummary, reverseMergeConfirmMessage, reverseMergeFailure, voidFailure, createErrorMessage, createFormErrors, dateRangeError, hasActiveFilters, isScopedToTransaction, listSearch, normalizeAmount, transactionsErrorMessage, trapTarget } from "../app/lib/transactionForm.js";

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

test("T-16 polish: actors, evidence, minus sign, admin durations and decimal commas", async () => {
  const { actorText, evidenceTypeText, auditActionText } = await import("../app/lib/labels.js");
  const { money } = await import("../app/lib/format.js");
  const { ratioLabel } = await import("../app/lib/cycleReview.js");
  const { duration, percent, ms } = await import("../app/lib/adminFormat.js");
  for (const code of ["WORKER", "TELEGRAM_CALLBACK", "USER", "SYSTEM", "TELEGRAM"]) assert.doesNotMatch(actorText(code), /[A-Z_]{4,}/);
  assert.equal(evidenceTypeText("MANUAL_ENTRY"), "Input manual");
  assert.equal(evidenceTypeText("SOME_NEW_KIND"), "Some new kind");
  assert.equal(auditActionText("Auto confirm screenshot row"), "Baris tangkapan layar dikonfirmasi otomatis");
  assert.equal(money("-2777998"), "\u2212Rp\u00a02.777.998");
  assert.equal(ratioLabel("-0.165"), "\u221216,5%");
  assert.equal(percent(1), "100,0%");
  assert.equal(ms(40800), "40,8 dtk");
  assert.equal(duration(3_660_000), "1 j 1 m");
  assert.equal(duration(8000), "8 dtk");
  assert.equal(duration(20_867_600), "5 j 47 m");
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
  assert.ok(editFormErrors({ description: "  " }, { description: "Lama" }).description);
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

test("void: the button is offered for PENDING and CONFIRMED only", () => {
  for (const status of ["PENDING", "CONFIRMED"]) assert.equal(canVoidTransaction({ id: "t1", status }), true, status);
  for (const status of ["NEEDS_REVIEW", "VOIDED", undefined]) assert.equal(canVoidTransaction({ id: "t1", status }), false, String(status));
  assert.equal(canVoidTransaction({ status: "CONFIRMED" }), false, "no id, no action");
  assert.equal(canVoidTransaction(null), false);
  assert.match(page, /\{canVoidTransaction\(selected\) && <button type="button" className="danger"[^>]*>[^<]*\{busyAction === "void" \? "Membatalkan…" : "Batalkan transaksi"\}<\/button>\}/);
});

test("void: the confirm dialog is danger-styled and says the row leaves the totals but stays in history", () => {
  assert.match(page, /confirmText: VOID_CONFIRM_MESSAGE, confirmLabel: "Ya, batalkan"/);
  assert.match(page, /await confirm\(confirmText, \{ confirmLabel, danger: true \}\)/);
  assert.match(VOID_CONFIRM_MESSAGE, /keluar dari semua total/);
  assert.match(VOID_CONFIRM_MESSAGE, /tetap tersimpan di riwayat/);
  assert.match(page, /\{dialogs\}/);
});

test("void: a refused or failed request is never reported as success", () => {
  const review = voidFailure(409, { error: "resolve this transaction through the review endpoint" });
  assert.equal(review.kind, "review");
  assert.match(review.message, /Tinjauan/);
  assert.doesNotMatch(review.message, /review endpoint/);
  assert.match(page, /\(actionNotice\.kind === "review" \|\| actionNotice\.inbox\) && <a className="button secondary" href="\/inbox\?view=transactions">Buka Tinjauan<\/a>/);
  const merged = voidFailure(409, { code: "MERGE_TARGET", error: "raw server text" });
  assert.equal(merged.kind, "error"); assert.notEqual(merged.reload, true, "nothing changed, so no reload");
  assert.match(merged.message, /penggabungan yang masih aktif/);
  assert.match(merged.message, /Pisahkan penggabungan/, "the refusal points at the drawer action");
  assert.doesNotMatch(merged.message, /raw server text|MERGE_TARGET/);
  const stale = voidFailure(409, { error: "invalid transaction status transition" });
  assert.equal(stale.kind, "error"); assert.equal(stale.reload, true);
  for (const status of [400, 401, 403, 404, 500, 502]) {
    const outcome = voidFailure(status, { error: "boom" });
    assert.equal(outcome.kind, "error", String(status));
    assert.doesNotMatch(outcome.message, /boom|^Transaksi dibatalkan/, "no raw API text, no success wording");
  }
  assert.match(voidFailure(500).message, /Coba lagi/);
  // Success is declared only from response.ok; a thrown fetch is an error that re-reads the record.
  assert.match(page, /response\.ok \? \{ kind: "success"/);
  assert.match(page, /catch \{ outcome = \{ kind: "error", message: "Koneksi terputus\./);
});

test("void: posts once, then reloads the drawer and the list", () => {
  assert.match(page, /url: `\/api\/v1\/transactions\/\$\{selected\.id\}\/void`/);
  assert.match(page, /await fetch\(url, \{ method: "POST" \}\)/);
  assert.match(page, /if \(actionPending\.current \|\| !id\) return;\s+actionPending\.current = true;/);
  assert.match(page, /finally \{ actionPending\.current = false; setBusyAction\(""\); \}/);
  assert.match(page, /const voidTransaction = \(\) => canVoidTransaction\(selected\) && runDrawerAction/);
  assert.match(page, /drawer\.open\(id\)[\s\S]{0,120}load\(\)/);
  assert.match(page, /openId\.current === id/, "a drawer closed or moved to another row is not reopened");
  // The ref follows the drawer from an effect, never from the render body.
  assert.match(page, /useEffect\(\(\) => \{ openId\.current = drawer\.id; setActionNotice\(null\); \}, \[drawer\.id\]\)/);
  assert.doesNotMatch(page, /^\s*openId\.current = drawer\.id;/m);
});

test("merge: the drawer lists active merges and offers Pisahkan penggabungan behind the confirm dialog", () => {
  const target = { id: "m1", role: "TARGET", otherLabel: "Kopi dobel", otherAmount: "25000" };
  const source = { id: "m2", role: "SOURCE", otherLabel: "Kopi", otherAmount: "25000" };
  assert.match(mergeSummary(target, value => `Rp ${value}`), /^Menyerap transaksi duplikat: Kopi dobel · Rp 25000\.$/);
  assert.match(mergeSummary(source), /^Digabung ke transaksi lain: Kopi · 25000\.$/);
  assert.match(mergeSummary({ id: "m3", role: "TARGET" }), /Transaksi lain/, "an unnamed counterpart still reads");
  const message = reverseMergeConfirmMessage(target);
  assert.match(message, /kembali ke Tinjauan/);
  assert.match(message, /Tidak ada transaksi yang dihapus/);
  assert.match(page, /selected\.activeMerges\?\.length > 0 && <section className="merge-links"/);
  assert.match(page, /<button type="button" className="secondary" disabled=\{Boolean\(busyAction\)\}[^>]*onClick=\{\(\) => reverseMerge\(merge\)\}>\{busyAction === merge\.id \? "Memisahkan…" : "Pisahkan penggabungan"\}<\/button>/);
  assert.match(page, /url: `\/api\/v1\/reconciliation-merges\/\$\{merge\.id\}\/reverse`/);
  assert.match(page, /confirmLabel: "Ya, pisahkan"/);
});

test("merge: a refused or failed reversal is never reported as success and re-reads when the record moved", () => {
  for (const status of [404, 409]) { const outcome = reverseMergeFailure(status); assert.equal(outcome.kind, "error"); assert.equal(outcome.reload, true, String(status)); }
  for (const status of [401, 403, 500, 502]) { const outcome = reverseMergeFailure(status); assert.equal(outcome.kind, "error"); assert.notEqual(outcome.reload, true); assert.doesNotMatch(outcome.message, /^Penggabungan dipisahkan/); }
  assert.match(reverseMergeFailure(500).message, /Coba lagi/);
  assert.match(page, /success: \{ message: REVERSE_MERGE_SUCCESS_MESSAGE, inbox: true \}/);
});

test("void: Escape inside the confirm dialog does not also close the drawer", () => {
  const hook = readFileSync(new URL("../app/components/useDrawerA11y.js", import.meta.url), "utf8");
  assert.match(hook, /event\.key === "Escape"\) \{ if \(!event\.target\?\.closest\?\.\("dialog"\)\) closeRef\.current\(\)/);
});

test("every transaction audit action written by Go has an Indonesian label", () => {
  const codes = ["CREATE", "UPDATE_DETAILS", "LINK_BANK_EMAIL_EVIDENCE", "LINK_RECEIPT_EVIDENCE", "LINK_SCREENSHOT_EVIDENCE", "AUTO_CONFIRM_RECEIPT", "AUTO_CONFIRM_SCREENSHOT_ROW", "CREATE_RECEIPT_REVIEW", "CREATE_SCREENSHOT_REVIEW", "CREATE_FROM_PAYSLIP", "CREATE_FROM_TELEGRAM", "REMEMBER_MERCHANT", "EDIT_TRANSACTION", "CONFIRM_PENDING_BATCH", "REVIEW_DISTINCT_EVENT", "REVIEW_INCOME_CHOICE", "STAGE_EVIDENCE_DISPUTE", "SET_SCREENSHOT_AMOUNT", "REOPEN_LEGACY_UNCATEGORIZED", "ROLLBACK_REOPEN_LEGACY_UNCATEGORIZED"];
  for (const code of codes) assert.ok(auditActionLabel[code], code);
  assert.equal(auditActionLabel.CORRECT_TRANSACTION_CLASSIFICATION, undefined);
  assert.equal(auditActionText("Link receipt evidence"), auditActionLabel.LINK_RECEIPT_EVIDENCE);
});
