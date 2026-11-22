import { test, expect, reply } from "../support/fixtures.js";
import { ledgerBasics, transaction } from "../support/data.js";

const suggestions = () => ({
  frequent: [
    { description: "Kopi susu", amount: "25000", type: "EXPENSE", categoryId: "cat-1", accountId: "account-1" },
    { description: "Beli bahan makanan bulanan di pasar minggu bersama ibu", amount: "120000", type: "EXPENSE", categoryId: "cat-1", accountId: "account-1" },
  ],
  recentCategories: ["cat-1"],
  descriptionMap: { "kopi susu": "cat-1" },
});

function quickApi(api) {
  api.use(request => {
    const { path, method } = request;
    if (path === "/api/v1/transactions/quick-suggestions") return suggestions();
    if (path === "/api/v1/transactions" && method === "POST") return { id: "tx-new" };
    if (path === "/api/v1/transactions") return [transaction("tx-1")];
    if (path === "/api/v1/documents") return [{ id: "doc-1", status: "SUCCEEDED", documentType: "RECEIPT", sourceType: "WEB_IMAGE", createdAt: "2026-09-06T10:00:00+07:00" }];
    if (path === "/api/v1/wealth/accounts") return [];
    return ledgerBasics(path);
  });
}

const sheet = page => page.locator("dialog.quick-record[open]");

async function open(page) {
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "Tambah transaksi", exact: true }).click();
  await sheet(page).waitFor();
}

test("the sheet opens on the manual form with no chooser, an example amount and the repeat chips", async ({ page, api }) => {
  quickApi(api);
  await open(page);
  const dialog = sheet(page);
  await expect(dialog.getByRole("heading")).toBeVisible();
  const amount = dialog.locator('input[name="amount"]');
  await expect(amount).toBeFocused();
  await expect(amount, "an example, never a prefilled-looking 0").toHaveAttribute("placeholder", "Contoh: 25.000");
  await expect(amount).toHaveValue("");
  await expect(dialog.getByRole("group", { name: "Ulangi cepat" })).toBeVisible();
  await expect(dialog.getByRole("group", { name: "Kategori" })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Lainnya…" })).toBeVisible();
  await expect(dialog.getByText("Berbayar"), "no badges").toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "Foto bukti" }), "Foto bukti sits beside Catat").toBeVisible();
  const chip = dialog.getByRole("group", { name: "Ulangi cepat" }).getByRole("button").nth(1);
  expect((await chip.textContent()).length, "a long description is shortened on its chip").toBeLessThan(60);
});

test("the amount groups thousands as it is typed and a typed description suggests the household's category", async ({ page, api }) => {
  quickApi(api);
  await open(page);
  const dialog = sheet(page);
  const amount = dialog.locator('input[name="amount"]');
  await amount.pressSequentially("1250000", { delay: 40 });
  await expect(amount).toHaveValue("1.250.000");
  await dialog.locator('input[name="description"]').fill("Kopi susu");
  await expect(dialog.getByRole("status").getByRole("button", { name: "Belanja rumah" })).toBeVisible();
});

test("saving posts what the API accepts, closes at once, and confirms with an undo", async ({ page, api }) => {
  quickApi(api);
  api.on("/api/v1/transactions/tx-new/void", () => ({}), { method: "POST" });
  await open(page);
  const dialog = sheet(page);
  await dialog.locator('input[name="amount"]').fill("25.000");
  await dialog.locator('input[name="description"]').fill("Kopi susu");
  await dialog.getByRole("group", { name: "Kategori" }).getByRole("button", { name: "Belanja rumah", exact: true }).first().click();
  await dialog.getByRole("button", { name: /^Catat$/ }).click();
  await expect(dialog).toHaveCount(0);
  const post = api.calls("/api/v1/transactions", "POST");
  expect(post).toHaveLength(1);
  expect(post[0].body).toMatchObject({ type: "EXPENSE", amount: "25000", description: "Kopi susu", categoryId: "cat-1" });
  const toast = page.getByRole("status").filter({ hasText: "Tercatat" });
  await expect(toast).toBeVisible();
  await toast.getByRole("button", { name: "Urungkan" }).click();
  await expect.poll(() => api.calls("/api/v1/transactions/tx-new/void", "POST").length).toBe(1);
});

test("an empty submit names what is missing and sends nothing", async ({ page, api }) => {
  quickApi(api);
  await open(page);
  const dialog = sheet(page);
  await dialog.getByRole("button", { name: /^Catat$/ }).click();
  await expect(dialog.locator('input[name="amount"]')).toHaveAttribute("aria-invalid", "true");
  await expect(dialog.locator('input[name="description"]')).toHaveAttribute("aria-invalid", "true");
  await expect(dialog.locator("#quick-amount-error")).toBeVisible();
  expect(api.calls("/api/v1/transactions", "POST")).toHaveLength(0);
});

test("the sheet is a native modal that traps Tab and returns focus to the button that opened it", async ({ page, api }) => {
  quickApi(api);
  await open(page);
  const dialog = sheet(page);
  for (let index = 0; index < 40; index += 1) await page.keyboard.press("Tab");
  expect(await dialog.evaluate(element => element.contains(document.activeElement)), "Tab stays inside the sheet").toBe(true);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Tambah transaksi", exact: true }), "focus returns to the opener").toBeFocused();
});

test("Foto bukti opens the photo flow with its limits and recent uploads in Indonesian", async ({ page, api }) => {
  quickApi(api);
  await open(page);
  const dialog = sheet(page);
  await dialog.getByRole("button", { name: "Foto bukti" }).click();
  await expect(dialog).toContainText(/10 MB/);
  await expect(dialog).toContainText("Struk");
  await expect(dialog).not.toContainText(/WEB_IMAGE|RECEIPT/);
  await expect(dialog.getByRole("link", { name: /Lihat semua riwayat foto bukti/ }).or(dialog.getByRole("button", { name: /Lihat semua riwayat foto bukti/ }))).toBeVisible();
});

test("a missing-transaction handoff limits the time to the residual cycle", async ({ page, api }) => {
  quickApi(api);
  await page.goto("/transactions?record=missing&from=2026-08-25&to=2026-09-24", { waitUntil: "networkidle" });
  const dialog = sheet(page);
  await dialog.waitFor();
  await expect(dialog).toContainText("2026-08-25 sampai 2026-09-24");
  const time = dialog.getByLabel("Waktu", { exact: true });
  await expect(time).toHaveAttribute("min", "2026-08-25T00:00");
  await expect(time).toHaveAttribute("max", "2026-09-24T23:59");
  await expect(time).toHaveValue("2026-09-07T10:00");
  await dialog.locator('input[name="amount"]').fill("10000");
  await dialog.locator('input[name="description"]').fill("Lupa dicatat");
  await time.fill("2026-09-30T10:00");
  await dialog.getByRole("button", { name: /^Catat$/ }).click();
  expect(api.calls("/api/v1/transactions", "POST"), "a time outside the cycle is refused").toHaveLength(0);
});

test("an upload that the server rejects keeps the form and says so", async ({ page, api }) => {
  quickApi(api);
  api.on("/api/v1/transactions", () => reply({ error: "amount must be positive" }, { status: 422 }), { method: "POST" });
  await open(page);
  const dialog = sheet(page);
  await dialog.locator('input[name="amount"]').fill("25.000");
  await dialog.locator('input[name="description"]').fill("Kopi");
  await dialog.getByRole("button", { name: /^Catat$/ }).click();
  await expect(dialog.getByRole("alert")).toBeVisible();
  await expect(dialog.getByRole("alert")).not.toContainText("amount");
  await expect(dialog.locator('input[name="description"]')).toHaveValue("Kopi");
});

test("a transfer asks for its purpose and source account, and a savings purpose also asks for the wealth account", async ({ page, api }) => {
  quickApi(api);
  api.on("/api/v1/wealth/accounts", () => [{ id: "wa-1", name: "Tabungan utama", wealthType: "BANK", active: true }]);
  await open(page);
  const dialog = sheet(page);
  await dialog.getByRole("group", { name: "Jenis transaksi" }).getByRole("button", { name: "Transfer" }).click();
  await expect(dialog.getByLabel("Tujuan transfer")).toBeVisible();
  await expect(dialog.getByLabel("Akun sumber")).toBeVisible();
  await expect(dialog.getByRole("group", { name: "Kategori" }), "a transfer has no category").toHaveCount(0);
  expect(await dialog.getByLabel("Tujuan transfer").locator("option").allTextContents()).toEqual(["Transfer internal", "Pindah ke tabungan", "Kontribusi investasi", "Pembelian aset", "Pembayaran pokok utang"]);
  await dialog.getByLabel("Tujuan transfer").selectOption("SAVINGS_TRANSFER");
  await expect(dialog.getByLabel("Akun kekayaan terkait")).toBeVisible();
  await dialog.locator('input[name="amount"]').fill("500.000");
  await dialog.locator('input[name="description"]').fill("Tabung");
  await dialog.getByRole("button", { name: /^Catat$/ }).click();
  await expect(dialog.getByLabel("Akun sumber")).toHaveAttribute("aria-invalid", "true");
  expect(api.calls("/api/v1/transactions", "POST")).toHaveLength(0);
});
