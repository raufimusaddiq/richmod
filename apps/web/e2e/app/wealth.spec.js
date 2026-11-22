import { test, expect, reply } from "../support/fixtures.js";

const accounts = () => [
  { id: "asset-1", name: "Tabungan", institution: "Bank", wealthType: "BANK", usageRole: "SPENDING", active: true },
  { id: "liability-1", name: "Kartu kredit", institution: "Bank", wealthType: "CREDIT_CARD", usageRole: "SPENDING", active: true },
];
const snapshot = (id, value) => ({ id, netWorthIdr: value, assetTotalIdr: value, liabilityTotalIdr: "0", observedAt: "2026-09-03T10:00:00+07:00", items: [{ wealthAccountId: "asset-1", name: "Tabungan", side: "ASSET", valueIdr: value }, { wealthAccountId: "liability-1", name: "Kartu kredit", side: "LIABILITY", valueIdr: "0" }] });

function wealthApi(api, { latest = snapshot("wealth-1", "1000000") } = {}) {
  api.use(({ path, method }) => {
    if (path === "/api/v1/wealth/accounts") return accounts();
    if (path === "/api/v1/wealth/summary") return latest ? { latest } : { latest: null };
    if (path === "/api/v1/wealth/snapshots/latest") return latest;
    if (path === "/api/v1/wealth/history") return latest ? [latest] : [];
    if (path === "/api/v1/wealth/current-cycle-savings") return { periodKind: "CURRENT_CYCLE", savingsAllocated: "0", periodStart: "2026-09-01", periodEnd: "2026-09-30", savingsByDestination: [] };
    if (/^\/api\/v1\/wealth\/snapshots\/wealth-\d+$/.test(path) && method === "GET") return latest;
    return undefined;
  });
}

test("a household with no snapshot is told Kekayaan is not set up and can create the first one", async ({ page, api }) => {
  wealthApi(api, { latest: null });
  await page.goto("/wealth", { waitUntil: "networkidle" });
  await expect(page.getByRole("heading", { name: "Kekayaan belum diatur", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Kelola akun kekayaan" })).toHaveAttribute("href", "/household?tab=akun#wealth-accounts");
});

test("a snapshot is saved with exact decimal strings, a manual source and a Jakarta timestamp", async ({ page, api }) => {
  wealthApi(api);
  api.on("/api/v1/wealth/snapshots", () => ({}), { method: "POST" });
  await page.goto("/wealth", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "Perbarui nilai", exact: true }).click();
  await expect(page.locator('input[name="value-asset-1"]'), "the form starts from the previous values").toHaveValue("1000000");
  await expect(page.locator('input[name="value-asset-1"]')).toHaveAttribute("inputmode", "numeric");
  await page.locator('input[name="value-asset-1"]').fill("1500000");
  await page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }).click();
  await expect.poll(() => api.calls("/api/v1/wealth/snapshots", "POST").length).toBe(1);
  const body = api.calls("/api/v1/wealth/snapshots", "POST")[0].body;
  expect(body.observedAt).toMatch(/\+07:00$/);
  expect(body.items[0]).toEqual({ wealthAccountId: "asset-1", valueIdr: "1500000", source: "MANUAL", note: null });
  expect(body.items[1]).toMatchObject({ wealthAccountId: "liability-1", source: "MANUAL" });
});

test("a failed save keeps the form and the old figures", { tag: "@desktop-only" }, async ({ page, api }) => {
  wealthApi(api);
  api.on("/api/v1/wealth/snapshots", () => reply({ error: "x" }, { status: 500 }), { method: "POST" });
  await page.goto("/wealth", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "Perbarui nilai", exact: true }).click();
  await page.locator('input[name="value-asset-1"]').fill("9999999");
  await page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Catatan posisi belum dapat disimpan.");
  await expect(page.locator('input[name="value-asset-1"]')).toHaveValue("9999999");
  await expect(page.locator(".wealth-position-primary > strong")).toHaveText(/1\.000\.000/);
});

test("a snapshot link opens its drawer, and a correction is sent to that snapshot", async ({ page, api }) => {
  wealthApi(api);
  api.on("/api/v1/wealth/snapshots/wealth-1", () => ({}), { method: "PUT" });
  await page.goto("/wealth?snapshotId=wealth-1", { waitUntil: "networkidle" });
  const drawer = page.getByRole("dialog");
  await expect(drawer).toContainText("Kekayaan bersih");
  await drawer.getByRole("button", { name: "Koreksi catatan" }).click();
  await drawer.locator('input[name="value-asset-1"]').fill("1200000");
  await drawer.getByRole("button", { name: "Simpan koreksi" }).click();
  await expect.poll(() => api.calls("/api/v1/wealth/snapshots/wealth-1", "PUT").length).toBe(1);
  expect(api.calls("/api/v1/wealth/snapshots/wealth-1", "PUT")[0].body.items[0]).toMatchObject({ wealthAccountId: "asset-1", valueIdr: "1200000" });
});

test("the snapshot drawer shows an Indonesian source label, not the raw MANUAL value", { tag: "@desktop-only" }, async ({ page, api }) => {
  const latest = { ...snapshot("wealth-1", "1000000"), items: [{ wealthAccountId: "asset-1", name: "Tabungan", side: "ASSET", valueIdr: "1000000", source: "MANUAL" }] };
  wealthApi(api, { latest });
  await page.goto("/wealth?snapshotId=wealth-1", { waitUntil: "networkidle" });
  const drawer = page.getByRole("dialog");
  await expect(drawer).toContainText("Input manual");
  await expect(drawer).not.toContainText("MANUAL");
});

test("the net worth history tooltip names the series Kekayaan bersih, not the raw data key", { tag: "@desktop-only" }, async ({ page, api }) => {
  wealthApi(api);
  const older = { ...snapshot("wealth-0", "800000"), observedAt: "2026-08-03T10:00:00+07:00" };
  api.use(({ path }) => (path === "/api/v1/wealth/history" ? [older, snapshot("wealth-1", "1000000")] : undefined));
  await page.goto("/wealth", { waitUntil: "networkidle" });
  const chart = page.locator(".chart-wrap").filter({ has: page.locator(".recharts-line") }).first();
  await chart.scrollIntoViewIfNeeded();
  const box = await chart.boundingBox();
  await page.mouse.move(box.x + box.width * 0.85, box.y + box.height / 2);
  const tooltip = page.locator(".recharts-tooltip-wrapper");
  await expect(tooltip).toContainText("Kekayaan bersih");
  await expect(tooltip).not.toContainText("value");
});
