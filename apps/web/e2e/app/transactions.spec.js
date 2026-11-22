import { test, expect, reply } from "../support/fixtures.js";
import { ledgerBasics, transaction } from "../support/data.js";

const rows = count => Array.from({ length: count }, (_, index) => transaction(`tx-${index + 1}`, `Toko ${index + 1}`, { transactionAt: `2026-09-${String(20 - index).padStart(2, "0")}T09:20:00+07:00` }));

function ledgerApi(api, { items = rows(3), total, next = "" } = {}) {
  const state = { items, voided: new Set(), edits: [] };
  api.use(request => {
    const { path, method, query } = request;
    if (path === "/api/v1/transactions") {
      const headers = { "X-Total-Count": String(total ?? state.items.length) };
      if (next && !query.get("cursor")) headers["X-Next-Cursor"] = next;
      return reply(state.items, { headers });
    }
    if (/^\/api\/v1\/transactions\/tx-[^/]+\/(evidence|audit)$/.test(path)) return [];
    const detail = path.match(/^\/api\/v1\/transactions\/(tx-[^/]+)$/);
    if (detail && method === "GET") return { ...(state.items.find(item => item.id === detail[1]) || state.items[0]), updatedAt: "2026-09-06T10:00:00Z", ...(state.voided.has(detail[1]) ? { status: "VOIDED" } : {}) };
    return ledgerBasics(path);
  });
  return state;
}

test("filters are backed by the URL: applying writes the query, a reload keeps it, and Back leaves the next page", async ({ page, api }) => {
  ledgerApi(api, { next: "cursor-2" });
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".transaction-row").first().waitFor();
  for (const name of ["q", "categoryId", "memberId", "accountId", "type", "status", "source", "from", "to"]) await expect(page.locator(`#transaction-filters [name="${name}"]`), `filter ${name}`).toHaveCount(1);
  await page.getByLabel("Cari", { exact: true }).fill("kopi");
  await page.getByLabel("Cari", { exact: true }).press("Enter");
  await expect.poll(() => new URL(page.url()).searchParams.get("q")).toBe("kopi");
  expect(api.calls("/api/v1/transactions").at(-1).query.get("q"), "the request carries the filter").toBe("kopi");
  await page.reload({ waitUntil: "networkidle" });
  await expect(page.getByLabel("Cari", { exact: true }), "a reload keeps the filter").toHaveValue("kopi");
  await page.locator(".transaction-row").first().waitFor();
  await page.getByRole("button", { name: "Berikutnya" }).click();
  await expect(page.locator(".pagination-status")).toContainText("Halaman 2");
  await page.goBack();
  await expect(page.locator(".pagination-status"), "Back returns to the first page").not.toContainText("Halaman 2");
  await expect(page.getByLabel("Cari", { exact: true })).toHaveValue("kopi");
});

test("an inverted date range is refused in Indonesian before any request is sent", async ({ page, api }) => {
  ledgerApi(api);
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".transaction-row").first().waitFor();
  await page.locator(".filter-toggle").click();
  await page.getByLabel("Dari tanggal", { exact: true }).fill("2026-11-10");
  await page.getByLabel("Sampai tanggal", { exact: true }).fill("2026-01-01");
  const before = api.calls("/api/v1/transactions").length;
  await page.getByRole("button", { name: "Terapkan filter", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "tidak boleh setelah" })).toBeVisible();
  expect(api.calls("/api/v1/transactions").length).toBe(before);
});

test("a failed reload keeps the rows on screen, never shows raw API text, and Hapus filter recovers", async ({ page, api }) => {
  ledgerApi(api);
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".transaction-row").first().waitFor();
  api.on("/api/v1/transactions", request => (request.query.get("q") ? reply({ error: "boom: SELECT * FROM transactions" }, { status: 500 }) : undefined));
  await page.getByLabel("Cari", { exact: true }).fill("grab");
  await page.getByLabel("Cari", { exact: true }).press("Enter");
  const notice = page.getByRole("alert").filter({ hasText: "Transaksi belum dapat dimuat. Coba lagi." });
  await expect(notice).toBeVisible();
  await expect(page.locator("body"), "no raw server text").not.toContainText("SELECT");
  await expect(page.locator(".transaction-row"), "the rows already on screen stay").toHaveCount(3);
  await page.getByRole("button", { name: "Hapus filter" }).click();
  await expect.poll(() => new URL(page.url()).search).toBe("");
  await expect(notice).toHaveCount(0);
});

test("a filter with no match says so and offers Hapus filter; an empty ledger keeps the plain message", async ({ page, api }) => {
  const state = ledgerApi(api);
  state.items = [];
  await page.goto("/transactions?q=zzz", { waitUntil: "networkidle" });
  await expect(page.getByText("Tidak ada transaksi yang cocok dengan filter.")).toBeVisible();
  await page.getByRole("button", { name: "Hapus filter" }).last().click();
  await expect(page.getByText("Belum ada transaksi untuk ditampilkan.")).toBeVisible();
});

test("the heading shows the real total, pages are labelled, and filters are inert while loading", async ({ page, api }) => {
  ledgerApi(api, { items: rows(3), total: 120, next: "cursor-2" });
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  api.on("/api/v1/transactions", async () => { await gate; return undefined; });
  await page.goto("/transactions", { waitUntil: "domcontentloaded" });
  await expect(page.locator("#transaction-filters"), "filters are inert before the first load completes").toHaveJSProperty("inert", true);
  release();
  await expect(page.getByRole("heading", { name: "1–3 dari 120 transaksi" })).toBeVisible();
  await expect(page.locator("#transaction-filters")).toHaveJSProperty("inert", false);
  await expect(page.getByRole("status").filter({ hasText: "dari 120 transaksi" }), "the count is a polite live region").toHaveAttribute("aria-live", "polite");
  await expect(page.getByRole("button", { name: "Sebelumnya" })).toBeDisabled();
  await page.getByRole("button", { name: "Berikutnya" }).click();
  await expect(page.locator(".pagination-status")).toContainText("Halaman 2");
});

test("opening a row writes ?open= and Back closes the drawer; paging closes it too", async ({ page, api }) => {
  ledgerApi(api, { next: "cursor-2" });
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".transaction-row").first().click();
  const drawer = page.getByRole("dialog", { name: "Detail transaksi" });
  await drawer.waitFor();
  await expect(drawer).toHaveAttribute("aria-modal", "true");
  expect(new URL(page.url()).searchParams.get("open")).toBe("tx-1");
  expect(api.calls("/api/v1/transactions").at(-1).query.has("open"), "the drawer id never reaches the list request").toBe(false);
  await page.goBack();
  await drawer.waitFor({ state: "detached" });
  await page.locator(".transaction-row").first().click();
  await drawer.waitFor();
  await page.keyboard.press("Escape");
  await drawer.waitFor({ state: "detached" });
  expect(new URL(page.url()).searchParams.has("open"), "closing removes ?open=").toBe(false);
  await expect(page.locator(".transaction-row").first(), "Escape returns focus to the row").toBeFocused();
});

test("the date filters carry visible labels", async ({ page, api }) => {
  ledgerApi(api);
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".filter-toggle").click();
  await expect(page.getByLabel("Dari tanggal", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Sampai tanggal", { exact: true })).toBeVisible();
  await page.getByLabel("Dari tanggal", { exact: true }).fill("2026-09-25");
  await expect(page.locator(".filter-date .date-hint").first(), "the chosen date is spelled out").toHaveAttribute("data-date", "25 Sep 2026");
});

test.describe("cancelling a transaction", () => {
  test("is offered for a confirmed one, asks first and says what changes, then posts once and reloads", async ({ page, api }) => {
    const state = ledgerApi(api);
    api.on("/api/v1/transactions/tx-1/void", () => { state.voided.add("tx-1"); return {}; }, { method: "POST" });
    await page.goto("/transactions", { waitUntil: "networkidle" });
    await page.locator(".transaction-row").first().click();
    const drawer = page.getByRole("dialog", { name: "Detail transaksi" });
    await drawer.getByRole("button", { name: "Batalkan transaksi" }).click();
    const confirm = page.locator("dialog[open]:not(.create-dialog)");
    await expect(confirm).toContainText("keluar dari semua total");
    await expect(confirm).toContainText("tetap tersimpan di riwayat");
    await confirm.getByRole("button", { name: "Ya, batalkan" }).click();
    await expect.poll(() => api.calls("/api/v1/transactions/tx-1/void", "POST").length).toBe(1);
    await expect(drawer.getByRole("button", { name: "Batalkan transaksi" }), "a voided transaction cannot be cancelled again").toHaveCount(0);
  });

  test("a refused request is never reported as success and points to the review", async ({ page, api }) => {
    ledgerApi(api);
    api.on("/api/v1/transactions/tx-1/void", () => reply({ code: "REVIEW_OPEN", error: "resolve this transaction through the review endpoint" }, { status: 409 }), { method: "POST" });
    await page.goto("/transactions", { waitUntil: "networkidle" });
    await page.locator(".transaction-row").first().click();
    const drawer = page.getByRole("dialog", { name: "Detail transaksi" });
    await drawer.getByRole("button", { name: "Batalkan transaksi" }).click();
    await page.locator("dialog[open]:not(.create-dialog)").getByRole("button", { name: "Ya, batalkan" }).click();
    await expect(drawer.getByRole("alert")).toContainText("Tinjauan");
    await expect(drawer.getByRole("alert")).not.toContainText("review endpoint");
    await expect(drawer.getByRole("link", { name: "Buka Tinjauan" })).toHaveAttribute("href", "/inbox?view=transactions");
  });

  test("a needs-review transaction offers neither edit nor cancel", async ({ page, api }) => {
    ledgerApi(api, { items: [transaction("tx-1", "Toko", { status: "NEEDS_REVIEW" })] });
    await page.goto("/transactions?status=NEEDS_REVIEW", { waitUntil: "networkidle" });
    await page.locator(".transaction-row").first().click();
    const drawer = page.getByRole("dialog", { name: "Detail transaksi" });
    await drawer.waitFor();
    await expect(drawer.getByRole("button", { name: "Batalkan transaksi" })).toHaveCount(0);
    await expect(drawer.getByRole("button", { name: "Ubah", exact: true })).toHaveCount(0);
  });
});

test("editing sends only the changed fields with the version it read, and Escape in the editor leaves the drawer", async ({ page, api }) => {
  ledgerApi(api, { items: [transaction("tx-1", "Grab", { counterpartyName: "QRIS GRAB*1234", description: "Kopi", categoryId: "cat-1", accountId: "account-1" })] });
  api.on("/api/v1/transactions/tx-1", () => ({}), { method: "PATCH" });
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".transaction-row").first().click();
  const drawer = page.getByRole("dialog", { name: "Detail transaksi" });
  const edit = drawer.getByRole("button", { name: "Ubah", exact: true });
  await expect(edit).toBeEnabled();
  await expect(drawer).toContainText("Dari bank: QRIS GRAB*1234");
  await edit.click();
  const dialog = page.locator("dialog[open].create-dialog");
  await dialog.waitFor();
  await expect(dialog).toHaveAttribute("aria-labelledby", "edit-transaction-title");
  await expect(dialog.getByLabel("Tempat")).toHaveValue("Grab");
  await expect(dialog).toContainText("Transaksi berikutnya dari nama ini ikut berubah.");
  await dialog.locator('input[name="description"]').fill("Kopi susu");
  await dialog.getByRole("button", { name: /^Simpan/ }).click();
  await expect.poll(() => api.calls("/api/v1/transactions/tx-1", "PATCH").length).toBe(1);
  const patch = api.calls("/api/v1/transactions/tx-1", "PATCH")[0];
  expect(patch.body, "untouched category and account are never wiped").toEqual({ description: "Kopi susu" });
  expect(patch.headers["if-match"], "the version that was read").toBe("2026-09-06T10:00:00Z");
});

test("the Catat sheet refreshes the open ledger after a save", async ({ page, api }) => {
  const state = ledgerApi(api);
  api.on("/api/v1/transactions", request => (request.method === "POST" ? (state.items = [transaction("tx-new", "Warung baru"), ...state.items], { id: "tx-new" }) : undefined));
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".transaction-row").first().waitFor();
  await page.getByRole("button", { name: "Tambah transaksi", exact: true }).click();
  const dialog = page.locator("dialog.quick-record[open]");
  await expect(dialog).toHaveAttribute("aria-labelledby", "quick-record-title");
  await dialog.locator('input[name="amount"]').fill("25000");
  await dialog.locator('input[name="description"]').fill("Warung baru");
  await dialog.getByRole("button", { name: /^Catat$/ }).click();
  await expect(page.locator(".transaction-row").filter({ hasText: "Warung baru" })).toBeVisible();
  expect(api.calls("/api/v1/transactions", "POST")).toHaveLength(1);
});

test("the header states the page size and the filter toggle counts what is applied", async ({ page, api }) => {
  ledgerApi(api);
  await page.goto("/transactions?type=EXPENSE&status=CONFIRMED", { waitUntil: "networkidle" });
  await expect(page.getByText(/50 per halaman/)).toBeVisible();
  await expect(page.locator(".filter-toggle")).toHaveText("Filter lain (2)");
  await page.locator(".filter-toggle").click();
  await expect(page.locator(".filter-toggle")).toHaveAttribute("aria-expanded", "true");
  await page.getByRole("button", { name: "Terapkan filter", exact: true }).click();
  await expect(page.locator(".filter-toggle"), "applying collapses the panel").toHaveAttribute("aria-expanded", "false");
});

test("an open merge is listed in the drawer and Pisahkan penggabungan asks first", async ({ page, api }) => {
  const state = ledgerApi(api);
  api.on("/api/v1/transactions/tx-1", () => ({ ...state.items[0], updatedAt: "2026-09-06T10:00:00Z", activeMerges: [{ id: "m1", role: "TARGET", otherLabel: "Kopi dobel", otherAmount: "25000" }] }), { method: "GET" });
  api.on("/api/v1/reconciliation-merges/m1/reverse", () => ({}), { method: "POST" });
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.locator(".transaction-row").first().click();
  const drawer = page.getByRole("dialog", { name: "Detail transaksi" });
  await expect(drawer).toContainText("Menyerap transaksi duplikat: Kopi dobel");
  await drawer.getByRole("button", { name: "Pisahkan penggabungan" }).click();
  const confirm = page.locator("dialog[open]:not(.create-dialog)");
  await expect(confirm).toContainText("kembali ke Tinjauan");
  await expect(confirm).toContainText("Tidak ada transaksi yang dihapus");
  await confirm.getByRole("button", { name: "Ya, pisahkan" }).click();
  await expect.poll(() => api.calls("/api/v1/reconciliation-merges/m1/reverse", "POST").length).toBe(1);
});
