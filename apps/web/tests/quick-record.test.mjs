import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { MAX_PHOTOS, QUICK_EVENTS, buildQuickPayload, categoryChips, caretAfterFormat, formatRupiahInput, normalizeDescription, photoSelectionError, photoStatus, quickFormErrors, rupiahDigits, sanitizeSuggestions, suggestCategory } from "../app/lib/quickRecord.js";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("the amount groups thousands live as it is typed", () => {
  assert.equal(formatRupiahInput("60000"), "60.000");
  assert.equal(formatRupiahInput("6"), "6");
  assert.equal(formatRupiahInput("1234567"), "1.234.567");
  assert.equal(formatRupiahInput("60.000"), "60.000", "re-formatting is stable");
  assert.equal(formatRupiahInput("60.0000"), "600.000", "typing another digit regroups");
  assert.equal(formatRupiahInput("12abc3"), "123", "non-digits are dropped");
  assert.equal(formatRupiahInput("000"), "", "leading zeros never survive");
  assert.equal(formatRupiahInput("0150"), "150");
  assert.equal(formatRupiahInput(""), "");
  assert.equal(formatRupiahInput(null), "");
  assert.equal(rupiahDigits("1.234.567"), "1234567", "the API gets whole IDR");
  assert.equal(rupiahDigits("Rp 60.000"), "60000");
});

test("the caret stays after the same digit once the value is regrouped", () => {
  assert.equal(caretAfterFormat("1.234", 0), 0);
  assert.equal(caretAfterFormat("1.234", 1), 1);
  assert.equal(caretAfterFormat("1.234", 2), 3, "past the dot");
  assert.equal(caretAfterFormat("1.234", 4), 5);
  assert.equal(caretAfterFormat("1.234", 9), 5);
});

test("descriptions normalise the way the API does", () => {
  assert.equal(normalizeDescription("  Kopi   PAGI "), "kopi pagi");
  assert.equal(normalizeDescription(""), "");
  assert.equal(normalizeDescription(undefined), "");
});

test("a typed description suggests the household's own category, and only an active one", () => {
  const map = { "kopi pagi": "food", "parkir mall": "transport", "gaji": "inactive" };
  const active = new Set(["food", "transport"]);
  assert.equal(suggestCategory(map, "Kopi  Pagi", active), "food", "exact, case and spacing insensitive");
  assert.equal(suggestCategory(map, "kopi", active), "food", "a prefix of a known description");
  assert.equal(suggestCategory(map, "parkir", active), "transport");
  assert.equal(suggestCategory(map, "ko", active), "", "under three characters never guesses");
  assert.equal(suggestCategory(map, "gaji", active), "", "an inactive category is never suggested");
  assert.equal(suggestCategory(map, "bensin", active), "");
  assert.equal(suggestCategory(map, "", active), "");
  assert.equal(suggestCategory(null, "kopi", active), "");
  assert.equal(suggestCategory(map, "kopi", ["food"]), "food", "an array of ids works too");
});

test("the 1-tap category chips are the recent active ones, at most six", () => {
  const categories = Array.from({ length: 9 }, (_, index) => ({ id: `c${index}`, name: `Kategori ${index}`, active: index !== 1 }));
  const recent = ["c0", "c1", "c2", "c2", "missing", "c3", "c4", "c5", "c6", "c7"];
  const chips = categoryChips(recent, categories);
  assert.deepEqual(chips.map(item => item.id), ["c0", "c2", "c3", "c4", "c5", "c6"]);
  assert.deepEqual(categoryChips([], categories), []);
  assert.deepEqual(categoryChips(recent, categories, 2).map(item => item.id), ["c0", "c2"]);
});

test("suggestions from the server are cleaned against what is still active", () => {
  const categories = [{ id: "food", active: true }, { id: "old", active: false }];
  const accounts = [{ id: "bank", active: true }, { id: "closed", active: false }];
  const clean = sanitizeSuggestions({
    frequent: [
      { description: "Kopi", amount: "25000", type: "EXPENSE", categoryId: "food", accountId: "bank" },
      { description: "Warung", amount: "12000", type: "EXPENSE", categoryId: "old", accountId: "closed" },
      { description: "Pindah", amount: "5000", type: "TRANSFER", categoryId: null, accountId: null },
      { description: "", amount: "1", type: "EXPENSE" },
      { description: "Bad amount", amount: "12.5", type: "EXPENSE" },
    ],
    recentCategories: ["food", "old"],
    descriptionMap: { kopi: "food" },
  }, { categories, accounts });
  assert.equal(clean.frequent.length, 2, "transfers, blanks and malformed amounts are dropped");
  assert.deepEqual([clean.frequent[1].categoryId, clean.frequent[1].accountId], [null, null], "an inactive category/account is cleared, the chip stays");
  assert.deepEqual(clean.recentCategories, ["food"]);
  assert.deepEqual(clean.descriptionMap, { kopi: "food" });
  assert.deepEqual(sanitizeSuggestions(null), { frequent: [], recentCategories: [], descriptionMap: {} }, "an unavailable endpoint is just no suggestions");
});

test("the payload matches what POST /transactions accepts", () => {
  const base = { type: "EXPENSE", amount: "60.000", description: " Makan siang ", categoryId: "food", accountId: "bank", purpose: "INTERNAL_TRANSFER", relatedWealthAccountId: "w1", note: "", transactionAt: "" };
  assert.deepEqual(buildQuickPayload(base), { type: "EXPENSE", amount: "60000", description: "Makan siang", accountId: "bank", categoryId: "food" });
  assert.deepEqual(buildQuickPayload({ ...base, type: "INCOME", note: "bonus" }), { type: "INCOME", amount: "60000", description: "Makan siang", accountId: "bank", categoryId: "food", note: "bonus" });
  const internal = buildQuickPayload({ ...base, type: "TRANSFER" });
  assert.deepEqual(internal, { type: "TRANSFER", amount: "60000", description: "Makan siang", accountId: "bank", purpose: "INTERNAL_TRANSFER" }, "a transfer has no category, and internal has no wealth account");
  const savings = buildQuickPayload({ ...base, type: "TRANSFER", purpose: "SAVINGS_TRANSFER" });
  assert.equal(savings.relatedWealthAccountId, "w1");
  assert.equal(savings.categoryId, undefined);
  const timed = buildQuickPayload({ ...base, transactionAt: "2026-11-05T10:30" });
  assert.match(timed.transactionAt, /^2026-11-05T\d{2}:30:00\.000Z$/);
});

test("validation mirrors the API and the cycle date lock", () => {
  assert.deepEqual(quickFormErrors({ type: "EXPENSE", amount: "60.000", description: "x" }), {});
  assert.ok(quickFormErrors({ type: "EXPENSE", amount: "", description: "x" }).amount);
  assert.ok(quickFormErrors({ type: "EXPENSE", amount: "0", description: "x" }).amount);
  assert.ok(quickFormErrors({ type: "EXPENSE", amount: "5", description: "  " }).description);
  assert.ok(quickFormErrors({ type: "TRANSFER", amount: "5", description: "x" }).accountId);
  assert.ok(quickFormErrors({ type: "TRANSFER", amount: "5", description: "x", accountId: "a", purpose: "ASSET_PURCHASE" }).relatedWealthAccountId);
  assert.deepEqual(quickFormErrors({ type: "TRANSFER", amount: "5", description: "x", accountId: "a", purpose: "INTERNAL_TRANSFER" }), {});
  assert.ok(quickFormErrors({ type: "EXPENSE", amount: "5", description: "x" }, { cycle: true }).transactionAt);
});

test("photo proof accepts up to ten JPG/PNG images and 10 MB, and only a failed one is retried", () => {
  const file = (name, size, type = "image/jpeg") => ({ name, size, type });
  assert.equal(photoSelectionError([file("a.jpg", 1000)]), "");
  assert.equal(photoSelectionError([file("a.png", 1000, "image/png")]), "");
  assert.ok(photoSelectionError([]));
  assert.ok(photoSelectionError([file("a.gif", 10, "image/gif")]), "only JPG and PNG");
  assert.ok(photoSelectionError(Array.from({ length: MAX_PHOTOS + 1 }, (_, index) => file(`${index}.jpg`, 10))), "more than ten images");
  assert.equal(photoSelectionError(Array.from({ length: MAX_PHOTOS }, (_, index) => file(`${index}.jpg`, 10))), "");
  assert.ok(photoSelectionError([file("a.jpg", 6 * 1024 * 1024), file("b.jpg", 5 * 1024 * 1024)]), "over 10 MB in total");
  assert.deepEqual(photoStatus({ status: "PROCESSING" }), { text: "Sedang diproses…", tone: "pending", retry: false });
  assert.equal(photoStatus({ status: "RECEIVED" }).tone, "pending");
  assert.equal(photoStatus({ status: "DONE", reprocessing: true }).tone, "pending");
  assert.deepEqual(photoStatus({ status: "FAILED" }), { text: "Gagal diproses", tone: "failed", retry: true });
  assert.equal(photoStatus({ status: "PROCESSED" }).retry, false);
});

test("the sheet opens on the manual form, with no chooser and no badges", () => {
  const dialog = read("app/components/QuickRecordDialog.js");
  assert.doesNotMatch(dialog, /Berbayar/);
  assert.match(dialog, /useState\(request\.mode\)/, "it opens on the requested mode, manual by default");
  assert.match(read("app/components/QuickRecordProvider.js"), /mode: detail\.mode === "photo" \? "photo" : "manual"/);
  assert.match(dialog, /inputMode="numeric"/);
  assert.match(dialog, /onChange=\{onAmount\}/);
  assert.match(dialog, /aria-label="Ulangi cepat"/);
  assert.match(dialog, /aria-label="Kategori"/);
  assert.match(dialog, /Lainnya…/);
  assert.match(dialog, /\/api\/v1\/transactions\/quick-suggestions/);
  assert.match(dialog, /Lihat semua riwayat foto bukti ›/);
  assert.match(dialog, /Proses ulang/);
  assert.match(dialog, /\/api\/v1\/documents/);
  assert.match(dialog, /<button type="button" className="secondary" onClick=\{\(\) => setMode\("photo"\)\}>Foto bukti<\/button><button type="submit" disabled=\{saving\}>/, "Foto bukti sits right beside Catat");
});

test("recent uploads use Indonesian document labels, never raw enum text", () => {
  const dialog = read("app/components/QuickRecordDialog.js");
  assert.match(dialog, /label\(documentTypeLabels, item\.documentType\) \|\| "Foto bukti"/);
  assert.doesNotMatch(dialog, /replaceAll\("_", " "\)/);
});

test("the provider opens from context even before its window listener exists", () => {
  const provider = read("app/components/QuickRecordProvider.js");
  assert.match(provider, /<QuickRecordContext\.Provider value=\{\{ open \}\}>/);
  assert.match(provider, /const open = useCallback\(/);
  assert.match(provider, /\{enabled && request && <QuickRecordDialog/);
});

test("saving closes at once, confirms with an undo, and tells open pages to refresh", () => {
  const provider = read("app/components/QuickRecordProvider.js");
  assert.equal(QUICK_EVENTS.created, "richmod:transaction-created");
  assert.equal(QUICK_EVENTS.open, "richmod:open-quick-record");
  assert.match(provider, /text: "Tercatat"/);
  assert.match(provider, />\{toast\.undoing \? "Membatalkan…" : "Urungkan"\}</);
  assert.match(provider, /\/api\/v1\/transactions\/\$\{toast\.id\}\/void/);
  assert.match(provider, /announceTransactionCreated\(\{ id: body\?\.id \|\| "" \}\);\s*close\(\);/);
  assert.match(read("app/layout.js"), /<QuickRecordProvider>/);
});

test("the sheet keeps the Retro Ledger rules: ink border, 16px inputs, sticky save bar, safe area", () => {
  const css = read("app/styles/04-ledger-and-drawers.css");
  assert.match(css, /\.quick-record\.create-dialog \{[^}]*border: 2px solid var\(--ink\)[^}]*box-shadow: var\(--shadow\)/);
  assert.match(css, /\.quick-body input, \.quick-body select, \.quick-body textarea \{[^}]*min-height: 44px; font-size: 16px;/);
  assert.match(css, /\.quick-savebar \{ position: sticky; bottom: 0;[^}]*env\(safe-area-inset-bottom\)/);
  assert.match(css, /\.quick-chips \.chip \{ min-height: 44px;/);
});
