import { test, expect, reply } from "../support/fixtures.js";
import { ledgerBasics, transaction } from "../support/data.js";

test.use({ viewport: { width: 1280, height: 800 } });

const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const leavingGone = page => page.waitForFunction(() => !document.querySelector(".presence[data-presence='leaving']"), null, { timeout: 3000 });

test.beforeEach(async ({ api }) => {
  api.use(request => {
    const { path } = request;
    if (path === "/api/v1/transactions") return reply([transaction("tx-1")], { delay: 400 });
    if (path.startsWith("/api/v1/transactions/tx-")) return path.endsWith("/evidence") || path.endsWith("/audit") ? [] : transaction("tx-1");
    return ledgerBasics(path);
  });
});

test("content that replaces the skeleton is revealed once, never nested", async ({ page }) => {
  await page.goto("/", { waitUntil: "networkidle" });
  await page.click(".sidebar nav a[href='/transactions']");
  await page.waitForSelector(".ledger-panel");
  const nested = await page.evaluate(() => ({ tagged: document.querySelectorAll("[data-reveal]").length, compounded: [...document.querySelectorAll("[data-reveal]")].filter(node => node.parentElement.closest("[data-reveal]")).length }));
  expect(nested.tagged, "content that replaces the skeleton is revealed").toBeGreaterThan(0);
  expect(nested.compounded, "a tagged node never sits inside another tagged node").toBe(0);
});

test("a drawer reopened while leaving is live and focused", async ({ page }) => {
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.waitForSelector(".ledger-panel");
  const row = page.locator(".ledger-panel button").first();
  await row.click();
  await page.waitForSelector(".detail-drawer");
  await pause(500);
  await page.click(".detail-drawer .drawer-close");
  await pause(60);
  await row.click();
  await page.waitForSelector(".presence[data-presence='open'] .detail-drawer");
  await pause(450);
  const reopened = await page.evaluate(() => {
    const presence = document.querySelector(".presence");
    return { inert: presence.inert, focusInside: presence.contains(document.activeElement), count: document.querySelectorAll(".detail-drawer").length };
  });
  expect(reopened, "a drawer reopened while leaving is live and focused").toEqual({ inert: false, focusInside: true, count: 1 });
});

test("a second confirm raised during the exit opens, and Escape in it leaves the drawer open", async ({ page }) => {
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.waitForSelector(".ledger-panel");
  await page.locator(".ledger-panel button").first().click();
  await page.waitForSelector(".detail-drawer");
  await pause(500);
  const voidButton = page.locator(".detail-drawer button", { hasText: "Batalkan transaksi" });
  await voidButton.click();
  await page.waitForSelector("dialog[open]:not(.create-dialog)");
  await pause(450);
  await page.keyboard.press("Escape");
  await pause(60);
  await voidButton.click();
  await page.waitForSelector("dialog[open]:not(.create-dialog)", { timeout: 2000 });
  await pause(450);
  await expect(page.locator("dialog[open]:not(.create-dialog)"), "a second confirm raised during the exit opens").toHaveCount(1);
  await page.keyboard.press("Escape");
  await leavingGone(page);
  expect(await page.locator(".detail-drawer").count(), "Escape in the confirm leaves the drawer open").toBeGreaterThan(0);
});

test("Catat reopened during its exit opens, and closing unmounts it", async ({ page }) => {
  await page.goto("/transactions", { waitUntil: "networkidle" });
  await page.waitForSelector(".ledger-panel");
  await page.click(".page-actions button:has-text('Tambah transaksi')");
  await page.waitForSelector("dialog.quick-record[open]");
  await pause(450);
  await page.click("dialog.quick-record .dialog-close");
  await pause(60);
  await page.click(".page-actions button:has-text('Tambah transaksi')");
  await page.waitForSelector("dialog.quick-record[open]", { timeout: 2000 });
  await pause(450);
  await expect(page.locator("dialog.quick-record[open]"), "Catat reopened during the exit opens").toHaveCount(1);
  await page.click("dialog.quick-record .dialog-close");
  await page.waitForFunction(() => !document.querySelector("dialog.quick-record"), null, { timeout: 3000 });
});
