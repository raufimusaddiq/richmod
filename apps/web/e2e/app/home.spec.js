import { test, expect, reply } from "../support/fixtures.js";
import { transaction } from "../support/data.js";

const overview = (overrides = {}) => ({ periodKind: "CURRENT_CYCLE", income: "12000000", expense: "4500000", netCashflow: "7500000", savingsAllocated: "1000000", unallocatedSurplus: "6500000", reviewCount: 0, ...overrides });
const daily = () => Array.from({ length: 6 }, (_, index) => ({ period: `2026-09-${String(index + 1).padStart(2, "0")}`, expense: String(100000 + index * 50000), refund: "0", grossExpense: String(100000 + index * 50000), income: "0" }));
const snapshot = { id: "wealth-9", netWorthIdr: "48250000", assetTotalIdr: "50000000", liabilityTotalIdr: "1750000", observedAt: "2026-09-09T15:00:00+07:00", items: [{ wealthAccountId: "wa-1", name: "Tabungan", side: "ASSET", valueIdr: "50000000" }, { wealthAccountId: "wa-3", name: "Kartu", side: "LIABILITY", valueIdr: "1750000" }] };

function homeApi(api, { latest = snapshot, ledger = [transaction("tx-1"), transaction("tx-2", "Grab")] } = {}) {
  api.use(({ path }) => {
    if (path === "/api/v1/analytics/overview") return overview();
    if (path === "/api/v1/analytics/cycle/daily") return { daily: daily(), daysElapsed: 6 };
    if (path === "/api/v1/analytics/categories") return [{ id: "c1", name: "Belanja rumah", amount: "2100000" }];
    if (path === "/api/v1/analytics/cycle") return { kind: "CURRENT_CYCLE", start: "2026-09-01", end: "2026-09-30" };
    if (path === "/api/v1/transactions") return ledger;
    if (path === "/api/v1/wealth/snapshots/latest") return latest;
    return undefined;
  });
}

test("a failed section is named and the rest of Beranda stays", async ({ page, api }) => {
  homeApi(api);
  api.on("/api/v1/wealth/snapshots/latest", () => reply({}, { status: 500 }));
  await page.goto("/", { waitUntil: "networkidle" });
  await expect(page.getByRole("alert").filter({ hasText: "Belum termuat: kekayaan. Bagian lain tetap ditampilkan." })).toBeVisible();
  await expect(page.locator(".home-hero-amount"), "the figures still render").toBeVisible();
  await expect(page.locator(".transaction-row")).toHaveCount(2);
});

test("the dashboard asks for the current cycle and offers no photo upload of its own", { tag: "@desktop-only" }, async ({ page, api }) => {
  homeApi(api);
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator(".home-hero-amount").waitFor();
  expect(api.calls("/api/v1/analytics/categories").some(request => request.query.get("period") === "current_cycle")).toBe(true);
  await expect(page.getByText("Unggah bukti"), "+ Catat owns photo upload").toHaveCount(0);
});

test("the wealth card links to the exact snapshot it summarises, and says so when none exists", { tag: "@desktop-only" }, async ({ page, api }) => {
  homeApi(api);
  await page.goto("/", { waitUntil: "networkidle" });
  test.skip(page.viewportSize().width <= 680, "the phone layout hides the wealth card");
  await expect(page.locator("a.overview-wealth")).toHaveAttribute("href", "/wealth?snapshotId=wealth-9");
  await expect(page.locator("a.overview-wealth"), "the date uses the shared Indonesian formatter").toContainText("9 Sep 2026");
  homeApi(api, { latest: null });
  await page.reload({ waitUntil: "networkidle" });
  await expect(page.locator("a.overview-wealth")).toHaveAttribute("href", "/wealth");
  await expect(page.locator("a.overview-wealth strong")).toHaveText("—");
});

test("the daily chart is a named image with a value table behind it", { tag: "@desktop-only" }, async ({ page, api }) => {
  homeApi(api);
  await page.goto("/", { waitUntil: "networkidle" });
  test.skip(page.viewportSize().width <= 680, "the phone layout hides the daily chart");
  const chart = page.locator(".daily-spend [role='img']").first();
  await expect(chart).toHaveAttribute("aria-label", /.+/);
  expect(await page.locator("[role='application']").count(), "no chart switches screen readers out of browse mode").toBe(0);
  const table = page.locator(".overview-trend details").first();
  await table.locator("summary").click();
  await expect(table.getByText("Pengeluaran bersih harian", { exact: true })).toBeAttached();
});

test("with no data it welcomes the household, and 'Foto bukti' opens the photo flow", async ({ page, api }) => {
  homeApi(api, { ledger: [] });
  api.on("/api/v1/analytics/overview", () => overview({ income: "0", expense: "0", netCashflow: "0", savingsAllocated: "0", unallocatedSurplus: "0" }));
  await page.goto("/", { waitUntil: "networkidle" });
  await expect(page.getByRole("heading", { name: "Mulai catat keuangan keluarga" })).toBeVisible();
  await page.getByRole("button", { name: /Foto bukti/ }).click();
  await expect(page.locator("dialog.quick-record[open]")).toContainText(/10 MB/);
});

test("saving a transaction elsewhere refreshes the dashboard", { tag: "@desktop-only" }, async ({ page, api }) => {
  homeApi(api);
  await page.goto("/", { waitUntil: "networkidle" });
  await page.locator(".home-hero-amount").waitFor();
  const before = api.calls("/api/v1/analytics/overview").length;
  await page.evaluate(() => window.dispatchEvent(new CustomEvent("richmod:transaction-created", { detail: { id: "tx-9" } })));
  await expect.poll(() => api.calls("/api/v1/analytics/overview").length).toBeGreaterThan(before);
});

test("setup work waiting is offered to the owner with a link to the right tab", { tag: "@desktop-only" }, async ({ page, api }) => {
  homeApi(api);
  api.on("/api/v1/integration-actions", () => [{ id: "a1" }, { id: "a2" }]);
  await page.goto("/", { waitUntil: "networkidle" });
  const notice = page.getByRole("link", { name: /2 langkah setup atau pemrosesan butuh perhatian/ });
  await expect(notice).toHaveAttribute("href", "/household?tab=sumber#tindakan");
});
