import { test, expect, reply } from "../support/fixtures.js";

const document = (index, overrides = {}) => ({
  id: `doc-${index}`,
  status: "SUCCEEDED",
  documentType: "RECEIPT",
  sourceType: "WEB_IMAGE",
  createdAt: "2026-09-06T10:00:00+07:00",
  confidence: 0.93,
  linkedTransactionIds: [],
  summary: { merchant: `Toko ${index}`, amount: "185000" },
  needsReview: false,
  ...overrides,
});

function documentsApi(api, items) {
  api.use(({ path, method }) => {
    if (path === "/api/v1/documents" && method === "GET") return items;
    if (/\/api\/v1\/documents\/[^/]+\/pages$/.test(path)) return [{ index: 0 }];
    if (/\/api\/v1\/documents\/[^/]+\/extraction$/.test(path)) return [{ stage: "RECEIPT", schemaVersion: 1, validated: true, output: { merchant: "Toko 1", amount: "185000" } }];
    return undefined;
  });
}

test("the list shows twelve at a time behind one 'Tampilkan lebih banyak', with lazy previews", async ({ page, api }) => {
  documentsApi(api, Array.from({ length: 30 }, (_, index) => document(index + 1)));
  await page.goto("/documents", { waitUntil: "networkidle" });
  await expect(page.locator(".document-card")).toHaveCount(12);
  await expect(page.getByRole("button", { name: "Tampilkan lebih banyak" }), "a single pagination control").toHaveCount(1);
  const image = page.locator(".document-card img").first();
  await expect(image).toHaveAttribute("alt", "Pratinjau dokumen keuangan");
  await expect(image).toHaveAttribute("loading", "lazy");
  await expect(image).toHaveAttribute("src", "/api/v1/documents/doc-1/thumbnail");
  await page.getByRole("button", { name: "Tampilkan lebih banyak" }).click();
  await expect(page.locator(".document-card")).toHaveCount(24);
});

test("status filters expose their state and say when nothing loaded matches", { tag: "@desktop-only" }, async ({ page, api }) => {
  documentsApi(api, [document(1), document(2)]);
  await page.goto("/documents", { waitUntil: "networkidle" });
  const filters = page.getByRole("group", { name: "Filter status dokumen" });
  await expect(filters.getByRole("button", { name: "Semua" })).toHaveAttribute("aria-pressed", "true");
  await filters.getByRole("button", { name: "Gagal" }).click();
  await expect(filters.getByRole("button", { name: "Gagal" })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("heading", { name: "Tidak ada dokumen pada filter ini" })).toBeVisible();
});

test("a failed card never claims success, shows Indonesian labels, and Proses ulang posts to the reprocess endpoint", { tag: "@desktop-only" }, async ({ page, api }) => {
  documentsApi(api, [document(1, { status: "FAILED", confidence: 0.9, summary: { merchant: "Tidak boleh tampil", amount: "1000" } }), document(2, { status: "EXTRACTED" })]);
  api.on("/api/v1/documents/doc-1/reprocess", () => ({}), { method: "POST" });
  await page.goto("/documents", { waitUntil: "networkidle" });
  const failed = page.locator(".document-card").first();
  await expect(failed).toContainText("Dokumen belum berhasil dibaca.");
  await expect(failed, "no payment summary on a failed card").not.toContainText("Tidak boleh tampil");
  await expect(failed, "no extraction score on a failed card").not.toContainText("Keyakinan ekstraksi");
  const ok = page.locator(".document-card").nth(1);
  await expect(ok).toContainText("Terbaca");
  await expect(page.locator("body")).not.toContainText(/WEB_IMAGE|EXTRACTED/);
  await failed.click();
  const drawer = page.getByRole("dialog", { name: "Detail dokumen" });
  await drawer.getByRole("button", { name: "Proses ulang" }).click();
  await expect.poll(() => api.calls("/api/v1/documents/doc-1/reprocess", "POST").length).toBe(1);
});

test("upload waits for a file and states the limits up front", { tag: "@desktop-only" }, async ({ page, api }) => {
  documentsApi(api, []);
  await page.goto("/documents", { waitUntil: "networkidle" });
  await expect(page.getByText("JPG atau PNG · maksimal 10 MB total · maksimal 10 gambar")).toBeVisible();
  const submit = page.getByRole("button", { name: "Unggah & kenali" });
  await expect(submit).toBeDisabled();
  await page.locator('input[type="file"]').setInputFiles({ name: "struk.png", mimeType: "image/png", buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64") });
  await expect(page.getByText(/1 gambar ·/)).toBeVisible();
  await expect(submit).toBeEnabled();
});

test("the document drawer is a labelled modal and its extraction text is a named, focusable region", async ({ page, api }) => {
  documentsApi(api, [document(1)]);
  await page.goto("/documents", { waitUntil: "networkidle" });
  await page.locator(".document-card").first().click();
  const drawer = page.getByRole("dialog", { name: "Detail dokumen" });
  await expect(drawer).toHaveAttribute("aria-modal", "true");
  await drawer.getByText("Detail teknis").click();
  const region = drawer.getByRole("region", { name: "Hasil ekstraksi, geser untuk semua isi" });
  await expect(region).toHaveAttribute("tabindex", "0");
  await page.keyboard.press("Escape");
  await drawer.waitFor({ state: "detached" });
  await expect(page.locator(".document-card").first(), "Escape returns focus to the card").toBeFocused();
});

test("a failed list can be retried and a failed drawer load says so", { tag: "@desktop-only" }, async ({ page, api }) => {
  let healthy = false;
  documentsApi(api, [document(1)]);
  api.on("/api/v1/documents", () => (healthy ? undefined : reply({}, { status: 500 })), { method: "GET" });
  await page.goto("/documents", { waitUntil: "networkidle" });
  await expect(page.getByRole("alert")).toBeVisible();
  healthy = true;
  await page.getByRole("button", { name: "Coba lagi" }).click();
  await expect(page.locator(".document-card")).toHaveCount(1);
  api.on(/\/extraction$/, () => reply({}, { status: 500 }));
  await page.locator(".document-card").first().click();
  await expect(page.getByRole("dialog", { name: "Detail dokumen" }).getByRole("alert")).toBeVisible();
});

test("a card title names the merchant and amount, counts extra rows, and falls back to source and date", { tag: "@desktop-only" }, async ({ page, api }) => {
  documentsApi(api, [
    document(1, { documentType: "EWALLET_SCREENSHOT", summary: { transactions: [{ merchant: "Kopi Kenangan", amount: "45000" }, { merchant: "X", amount: "1" }] } }),
    document(2, { summary: { merchant: "Toko", total: "45000.00" } }),
    document(3, { summary: { merchant: "Toko tanpa jumlah", total: "abc" } }),
    document(4, { status: "FAILED", summary: { transactions: [{ merchant: "Z" }] } }),
  ]);
  await page.goto("/documents", { waitUntil: "networkidle" });
  const titles = (await page.locator(".document-card h2").allTextContents()).map(text => text.replace(/\s/g, " "));
  expect(titles[0]).toBe("Kopi Kenangan · Rp 45.000 · 2 transaksi");
  expect(titles[1]).toBe("Toko · Rp 45.000");
  expect(titles[2]).toBe("Toko tanpa jumlah");
  expect(titles[3], "a failed card ignores its summary").toMatch(/^Unggahan web 6 Sep 2026/);
});

test("the drawer shows a payment-neutral summary line and the shared labels", { tag: "@desktop-only" }, async ({ page, api }) => {
  documentsApi(api, [document(1, { status: "NEEDS_REVIEW", needsReview: true, summary: { merchant: "Indomaret", total: "34000" } })]);
  await page.goto("/documents", { waitUntil: "networkidle" });
  const card = page.locator(".document-card").first();
  await expect(card).toContainText("Indomaret · Rp 34.000");
  await expect(card.locator(".status")).toHaveText("Perlu ditinjau");
  await expect(card.getByText("Perlu ditinjau"), "no second label under the badge").toHaveCount(1);
});
