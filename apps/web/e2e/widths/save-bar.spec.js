import { test, expect } from "../support/fixtures.js";

const accounts = () => [
  { id: "asset-1", name: "Tabungan", institution: "Bank", wealthType: "BANK", usageRole: "SPENDING", active: true },
  { id: "liability-1", name: "Kartu kredit", institution: "Bank", wealthType: "CREDIT_CARD", usageRole: "SPENDING", active: true },
];
const snapshot = (id, value) => ({ id, netWorthIdr: value, assetTotalIdr: value, liabilityTotalIdr: "0", observedAt: "2026-09-03T10:00:00+07:00", items: [{ wealthAccountId: "asset-1", name: "Tabungan", side: "ASSET", valueIdr: value }, { wealthAccountId: "liability-1", name: "Kartu kredit", side: "LIABILITY", valueIdr: "0" }] });

function wealthApi(api) {
  const latest = snapshot("wealth-1", "1000000");
  api.use(({ path, method }) => {
    if (path === "/api/v1/wealth/accounts") return accounts();
    if (path === "/api/v1/wealth/summary") return { latest };
    if (path === "/api/v1/wealth/snapshots/latest") return latest;
    if (path === "/api/v1/wealth/history") return [latest];
    if (path === "/api/v1/wealth/current-cycle-savings") return { periodKind: "CURRENT_CYCLE", savingsAllocated: "0", periodStart: "2026-09-01", periodEnd: "2026-09-30", savingsByDestination: [] };
    if (/^\/api\/v1\/wealth\/snapshots\/wealth-\d+$/.test(path) && method === "GET") return latest;
    return undefined;
  });
  api.on("/api/v1/wealth/snapshots", () => ({}), { method: "POST" });
}

const saves = api => api.calls("/api/v1/wealth/snapshots", "POST").length;

async function openForm(page) {
  await page.goto("/wealth", { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "Perbarui nilai", exact: true }).click();
  const input = page.locator('input[name="value-asset-1"]');
  await input.fill("1500000");
  return { input, save: page.getByRole("button", { name: "Simpan catatan lengkap", exact: true }) };
}

async function centreOf(locator, { scroll = false } = {}) {
  if (scroll) await locator.evaluate(element => element.scrollIntoView({ block: "center" }));
  const box = await locator.boundingBox();
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

async function expectSaveReachable(page, save, message, options) {
  const point = await centreOf(save, options);
  expect(point.y, "save button is on screen").toBeLessThan(page.viewportSize().height);
  const reachable = await save.evaluate((element, { x, y }) => element.contains(document.elementFromPoint(x, y)), point);
  expect(reachable, message).toBe(true);
  return point;
}

test.describe("mouse at 390px", () => {
  test.use({ viewport: { width: 390, height: 900 } });
  test.beforeEach(({ api }) => wealthApi(api));

  test("the save button is never under the bottom nav, focused or not, and a mouse click saves", async ({ page, api }) => {
    const { input, save } = await openForm(page);
    await expect(input).toBeFocused();
    await expectSaveReachable(page, save, "save button reachable while the input is focused");

    const point = await expectSaveReachable(page, save, "save button reachable before the press");
    await page.mouse.move(point.x, point.y);
    await page.mouse.down();
    await expect(input).not.toBeFocused();
    await expect(save).toBeFocused();
    await expectSaveReachable(page, save, "save button reachable after the press blurs the input");
    await page.mouse.up();
    await expect.poll(() => saves(api)).toBe(1);
  });

  test("with focus elsewhere the save button sits clear of the bottom nav", async ({ page }) => {
    const { input, save } = await openForm(page);
    await input.evaluate(element => element.blur());
    await expect(page.locator(".mobile-nav")).toBeVisible();
    await expectSaveReachable(page, save, "save button reachable after blur", { scroll: true });
  });
});

test.describe("touch at 390px", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });
  test.beforeEach(({ api }) => wealthApi(api));

  test("a tap on the save button saves", async ({ page, api }) => {
    const { save } = await openForm(page);
    await expectSaveReachable(page, save, "save button reachable while the input is focused");
    await save.tap();
    await expect.poll(() => saves(api)).toBe(1);
  });
});

test.describe("keyboard at 390px", () => {
  test.use({ viewport: { width: 390, height: 900 } });
  test.beforeEach(({ api }) => wealthApi(api));

  test("Tab reaches the save button and Enter saves", async ({ page, api }) => {
    const { input, save } = await openForm(page);
    await input.focus();
    for (let presses = 0; presses < 12 && !(await save.evaluate(element => element === document.activeElement)); presses += 1) await page.keyboard.press("Tab");
    await expect(save).toBeFocused();
    await expectSaveReachable(page, save, "save button reachable while focused by keyboard");
    await page.keyboard.press("Enter");
    await expect.poll(() => saves(api)).toBe(1);
  });
});

test.describe("settings forms at 390px", () => {
  test.use({ viewport: { width: 390, height: 900 } });

  test("the bottom nav stays hidden while focus moves from a field to the form's submit button", async ({ page }) => {
    await page.goto("/household?tab=akun", { waitUntil: "networkidle" });
    const input = page.getByRole("textbox", { name: "Nama rekening" });
    const add = page.locator(".inline-form").filter({ has: input }).getByRole("button", { name: "Tambah", exact: true });
    await input.fill("Rekening baru");
    await expect(page.locator(".mobile-nav")).toBeHidden();
    await add.focus();
    await expect(page.locator(".mobile-nav")).toBeHidden();
    await add.blur();
    await expect(page.locator(".mobile-nav")).toBeVisible();
  });
});
