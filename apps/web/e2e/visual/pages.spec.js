import { test, expect } from "../support/fixtures.js";
import { visualApi, visualUser } from "../support/visualApi.js";

test.use({ user: visualUser() });

const shot = (page, name) => expect(page).toHaveScreenshot(`${name}.png`, { fullPage: true });

const desktopStates = [
  ["analytics-cycle-closed-desktop", "/analytics?cycle=2026-08-26", {}, page => page.getByRole("heading", { name: "Ritme harian" })],
  ["analytics-cycle-category-desktop", "/analytics?category=11111111-1111-4111-8111-111111111111", {}, page => page.getByRole("heading", { name: "Belanja rumah", exact: true })],
  ["analytics-cycle-few-desktop", "/analytics", { fewCycles: true }, page => page.locator("#history")],
  ["analytics-cycle-first-desktop", "/analytics", { firstCycle: true }, page => page.getByRole("heading", { name: "Ritme harian" })],
  ["analytics-cycle-nowealth-desktop", "/analytics", { noWealth: true }, page => page.getByRole("heading", { name: "Ritme harian" })],
  ["analytics-cycle-nospending-desktop", "/analytics", { noSpending: true }, page => page.getByRole("heading", { name: "Ritme harian" })],
  ["analytics-cycle-full-desktop", "/analytics", { fullCycle: true }, page => page.getByRole("heading", { name: "Ritme harian" })],
];

const mobileStates = [
  ["analytics-cycle-first-mobile", { firstCycle: true }],
  ["analytics-cycle-nowealth-mobile", { noWealth: true }],
  ["analytics-cycle-nospending-mobile", { noSpending: true }],
  ["analytics-cycle-full-mobile", { fullCycle: true }],
];

test.describe("desktop 1440x900", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test.beforeEach(({ api }) => visualApi(api));

  for (const [name, path] of [["overview-desktop", "/"], ["transactions-desktop", "/transactions"], ["inbox-desktop", "/inbox"], ["analytics-cycle-desktop", "/analytics"]]) {
    test(name, async ({ page }) => {
      await page.goto(path, { waitUntil: "networkidle" });
      await page.locator("#main-content").waitFor();
      await shot(page, name);
    });
  }

  test("analytics-calendar-desktop", async ({ page }) => {
    await page.goto("/analytics", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Kalender" }).click();
    await page.getByRole("heading", { name: "Pemasukan vs pengeluaran" }).waitFor();
    await shot(page, "analytics-calendar-desktop");
  });

  test("analytics-cycle-tooltip-desktop", async ({ page }) => {
    await page.goto("/analytics", { waitUntil: "networkidle" });
    await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
    await page.locator(".cycle-spending-chart .recharts-bar-rectangle").first().hover();
    await page.locator(".chart-tooltip").waitFor();
    await expect(page).toHaveScreenshot("analytics-cycle-tooltip-desktop.png");
  });

  for (const [name, path, scenario, ready] of desktopStates) {
    test(name, async ({ page, api }) => {
      visualApi(api, { scenario });
      await page.goto(path, { waitUntil: "networkidle" });
      await ready(page).waitFor();
      if (name === "analytics-cycle-few-desktop") {
        await page.evaluate(() => { document.querySelector("#history details").open = true; });
        await page.locator(".heat-table").waitFor();
      }
      await shot(page, name);
    });
  }
});

test.describe("wide 1920x1080", () => {
  test.use({ viewport: { width: 1920, height: 1080 } });

  test("overview-wide-desktop", async ({ page, api }) => {
    visualApi(api);
    await page.goto("/", { waitUntil: "networkidle" });
    await page.locator("#main-content").waitFor();
    await shot(page, "overview-wide-desktop");
  });
});

test.describe("mobile 390x844", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test.beforeEach(({ api }) => visualApi(api));

  test("analytics-cycle-mobile", async ({ page }) => {
    await page.goto("/analytics", { waitUntil: "networkidle" });
    await page.locator("#main-content").waitFor();
    await shot(page, "analytics-cycle-mobile");
  });

  test("analytics-calendar-mobile", async ({ page }) => {
    await page.goto("/analytics", { waitUntil: "networkidle" });
    await page.locator("#main-content").waitFor();
    await page.getByRole("button", { name: "Kalender" }).click();
    await page.getByRole("heading", { name: "Pemasukan vs pengeluaran" }).waitFor();
    await shot(page, "analytics-calendar-mobile");
  });

  for (const [name, scenario] of mobileStates) {
    test(name, async ({ page, api }) => {
      visualApi(api, { scenario });
      await page.goto("/analytics", { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: "Ritme harian" }).waitFor();
      await shot(page, name);
    });
  }
});

test.describe("signed out", () => {
  test.beforeEach(({ api }) => visualApi(api, { authenticated: false }));

  for (const [name, path, width, height, heading] of [["landing-desktop", "/", 1440, 900, "Keuangan keluarga, tanpa menebak."], ["landing-tablet", "/", 1024, 768, "Keuangan keluarga, tanpa menebak."], ["landing-mobile", "/", 390, 844, "Keuangan keluarga, tanpa menebak."], ["login-desktop", "/login", 1440, 900, "Masuk ke Richmod."], ["login-mobile", "/login", 390, 844, "Masuk ke Richmod."]]) {
    test(name, async ({ page }) => {
      await page.setViewportSize({ width, height });
      await page.goto(path, { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: heading }).waitFor();
      await shot(page, name);
    });
  }
});
