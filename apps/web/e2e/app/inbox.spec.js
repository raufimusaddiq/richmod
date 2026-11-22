import { test, expect, reply } from "../support/fixtures.js";
import { layoutApi } from "../support/layoutApi.js";

const review = (overrides = {}) => ({
  id: "r1",
  reviewItemId: "ri1",
  reason: "AMBIGUOUS_CATEGORY",
  type: "EXPENSE",
  presentation: "DECISION",
  amount: "750000",
  merchantName: "Warung Bu Tini",
  transactionAt: "2026-09-05T15:00:00+07:00",
  sourceType: "BANK_EMAIL",
  proposalStatus: "NEEDS_REVIEW",
  missingFacts: ["category"],
  allowedActions: ["CONFIRM"],
  proposedFacts: { merchantName: "Warung Bu Tini", amount: "750000", categoryId: "cat-1" },
  suggestedCategories: [{ id: "cat-1", name: "Kategori Keluarga 1", count: 3, reason: "MERCHANT_HISTORY" }],
  description: "Pembayaran warung",
  ...overrides,
});

test.beforeEach(({ api }) => layoutApi(api));

test("a card says what Richmod proposes, what is still unknown, and keeps evidence secondary and editing behind a step", async ({ page, api }) => {
  api.on("/api/v1/reviews", () => [review()]);
  await page.goto("/inbox", { waitUntil: "networkidle" });
  const card = page.locator("#inbox-panel article, #inbox-panel section").filter({ hasText: "Warung Bu Tini" }).first();
  await expect(card).toContainText("Richmod mengusulkan");
  await expect(card).toContainText("Yang belum pasti: Kategori");
  await expect(card.getByRole("link", { name: "Bukti" }), "evidence is a secondary link, not part of the decision").toHaveAttribute("href", "/transactions?id=r1");
  await expect(card.getByRole("button", { name: "Ubah detail" })).toBeVisible();
  await expect(card.getByRole("button", { name: "Batal" }), "full editing is gated behind an explicit step").toHaveCount(0);
  await card.getByRole("button", { name: "Ubah detail" }).click();
  await expect(card.getByRole("button", { name: "Batal" })).toBeVisible();
  await card.getByRole("button", { name: "Batal" }).click();
  await expect(card.getByRole("button", { name: "Batal" })).toHaveCount(0);
  await expect(card.getByRole("button", { name: "Ubah detail" })).toBeVisible();
});

test("a one-tap category answer resolves the card, confirms in a line with Urungkan, and Urungkan reopens it", async ({ page, api }) => {
  let open = true;
  api.on("/api/v1/reviews", () => (open ? [review()] : []));
  api.on(/^\/api\/v1\/reviews\/r1\/(?!reopen)[a-z-]+$/, () => { open = false; return {}; }, { method: "POST" });
  api.on(/\/api\/v1\/reviews\/[^/]+\/reopen$/, () => { open = true; return {}; }, { method: "POST" });
  await page.goto("/inbox", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: /^Kategori Keluarga 1/ }).click();
  await expect(page.getByText("Semua sudah tercatat rapi")).toBeVisible();
  await expect(page.locator("#inbox-panel").getByText(/Warung Bu Tini · Rp.750.000 → /).first(), "the line says what was recorded").toBeVisible();
  await page.getByRole("button", { name: "Urungkan" }).click();
  await expect.poll(() => api.calls(/\/reopen$/, "POST").length).toBe(1);
  await expect(page.getByText("Warung Bu Tini").first()).toBeVisible();
});

test("a refused resolution is shown and the card stays", { tag: "@desktop-only" }, async ({ page, api }) => {
  api.on("/api/v1/reviews", () => [review()]);
  api.on(/^\/api\/v1\/reviews\/r1\/[a-z-]+$/, () => reply({ error: "Kategori tidak valid." }, { status: 422 }), { method: "POST" });
  await page.goto("/inbox", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: /^Kategori Keluarga 1/ }).click();
  await expect(page.getByRole("alert")).toContainText("Kategori tidak valid.");
  await expect(page.getByText("Warung Bu Tini").first()).toBeVisible();
});

test("an expense card offers the asset-purchase route without a dead switch", { tag: "@desktop-only" }, async ({ page, api }) => {
  api.on("/api/v1/reviews", () => [review()]);
  await page.goto("/inbox", { waitUntil: "networkidle" });
  const more = page.locator("details.review-more");
  await expect(more.locator("summary")).toHaveText("Atau catat sebagai beli aset");
  await more.locator("summary").click();
  await expect(more.getByRole("button").first()).toBeVisible();
});

test("an empty list says everything is recorded and links on, and history lists the last 30 days", { tag: "@desktop-only" }, async ({ page, api }) => {
  api.on("/api/v1/reviews", () => []);
  await page.goto("/inbox", { waitUntil: "networkidle" });
  await expect(page.getByRole("heading", { name: "Semua sudah tercatat rapi" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Ke Beranda" })).toHaveAttribute("href", "/");
  await expect(page.getByRole("link", { name: "Lihat Transaksi" })).toHaveAttribute("href", "/transactions");
  await expect(page.getByRole("tab", { name: /^Menunggu \(0\)/ })).toBeVisible();
  await page.getByRole("tab", { name: "Riwayat 30 hari" }).click();
  await expect(page.getByRole("tab", { name: "Riwayat 30 hari" })).toHaveAttribute("aria-selected", "true");
});

test("a card with a missing transaction date asks for one native date", { tag: "@desktop-only" }, async ({ page, api }) => {
  api.on("/api/v1/reviews", () => [review({ reason: "MISSING_TRANSACTION_DATE", missingFacts: ["transaction_at"], suggestedCategories: [], proposedFacts: {} })]);
  await page.goto("/inbox", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "Ubah detail" }).click();
  await expect(page.locator('input[name="transactionAt"]')).toHaveAttribute("type", /date/);
});
